package xml2nats

import (
	"encoding/xml"
	"flag"
	"log"
	"net"
	"time"

	"github.com/ClinicalSystemsEngineering/nats"
	"github.com/ClinicalSystemsEngineering/webadmin"
	"github.com/nats-io/go-nats-streaming"
	"gopkg.in/natefinch/lumberjack.v2" //rotational logging
)

//Page is the r5 xml structure.  although an r5 message contains Type it has been omitted for now.
type Page struct {
	ID      string `xml:"ID"`
	TagText string `xml:"TagText"`
	//Type string `xml:"Type"`
}

var parsedmsgs = make(chan string, 10000) //message processing channel for xml2nats conversions

var timeoutDuration = 5 * time.Second //read / write timeout duration

func main() {

	//r5 xml and web admin ports
	xmlPort := flag.String("xmlPort", "5051", "xml listener port for localhost")
	httpPort := flag.String("httpPort", "80", "localhost listner port for http server")

	//natspub flags
	var clusterID string
	var clientID string
	var async bool
	var URL string
	var subj string
	flag.StringVar(&URL, "s", stan.DefaultNatsURL, "The nats server URLs (separated by comma)")
	flag.StringVar(&URL, "server", stan.DefaultNatsURL, "The nats server URLs (separated by comma)")
	flag.StringVar(&clusterID, "c", "test-cluster", "The NATS Streaming cluster ID")
	flag.StringVar(&clusterID, "cluster", "test-cluster", "The NATS Streaming cluster ID")
	flag.StringVar(&clientID, "id", "stan-pub", "The NATS Streaming client ID to connect with")
	flag.StringVar(&clientID, "clientid", "stan-pub", "The NATS Streaming client ID to connect with")
	flag.BoolVar(&async, "a", false, "Publish asynchronously")
	flag.BoolVar(&async, "async", false, "Publish asynchronously")
	flag.StringVar(&subj, "subj", "Hospital.System", "Name of subject to publish to")
	flag.Parse()

	log.SetOutput(&lumberjack.Logger{
		Filename:   "/var/log/xml2tap/xml2tap.log",
		MaxSize:    100, // megabytes
		MaxBackups: 5,
		MaxAge:     60,   //days
		Compress:   true, // disabled by default
	})

	log.Printf("STARTING XML Listener on tcp port %v\n\n", *xmlPort)
	l, err := net.Listen("tcp", ":"+*xmlPort)
	if err != nil {
		log.Println("Error opening XML listener, check log for details")
		log.Fatal(err)
	}
	defer l.Close()

	//start a webserver for a web admin
	go webadmin.Webserver(*httpPort)

	go natspub.Pubber(clusterID, clientID, async, URL, parsedmsgs, subj)
	for {

		// Listen for an incoming xml connection.
		conn, err := l.Accept()
		if err != nil {
			log.Println("Error accepting: ", err.Error())
			log.Fatal(err)
		}

		// Handle connections in a new goroutine.
		go func(c net.Conn, msgs chan<- string) {
			//set up a decoder on the stream
			d := xml.NewDecoder(c)

			for {
				// Look for the next token
				// Note that this only reads until the next positively identified
				// XML token in the stream
				t, err := d.Token()
				if err != nil {
					log.Printf("Token error %v\n", err.Error())
					break
				}
				switch et := t.(type) {

				case xml.StartElement:
					// search for Page start element and decode
					if et.Name.Local == "Page" {
						p := &Page{}
						// decode the page element while automagically advancing the stream
						// if no matching token is found, there will be an error
						// note the search only happens within the parent.
						if err := d.DecodeElement(&p, &et); err != nil {
							log.Printf("error decoding element %v\n", err.Error())
							c.Close()
							return
						}

						// We have decoded the xml message now send it off to TAP server or reply if ping
						log.Printf("Parsed: Pin:%v;Msg:%v\n", p.ID, p.TagText)

						//note the R5 system periodically sends out a PING looking for a response
						//this will handle that response or put the decoded xml into the TAP output queue
						if p.ID == "" && p.TagText == "___PING___" {
							//send response to connection
							response := "<?xml version=\"1.0\" encoding=\"utf-8\"?> <PageTXSrvResp State=\"7\" PagesInQueue=\"0\" PageOK=\"1\" />"
							log.Printf("Responding:%v\n", response)
							c.SetWriteDeadline(time.Now().Add(timeoutDuration))
							_, err = c.Write([]byte(response))
							if err != nil {
								log.Println("Timeout error writing PING response")
								return
							}

						} else {
							parsedmsgs <- string(p.ID) + ";" + string(p.TagText)

						}

					}

				case xml.EndElement:
					if et.Name.Local != "Page" {
						continue
					}
				}

			}

			c.Close()
		}(conn, parsedmsgs)
	}

}
