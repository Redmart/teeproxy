package main

import (
	"crypto/tls"
	"flag"
	"log"
	"math/rand"
	"net"
	"net/http"
	"runtime"
	"time"

	proxy "./http"

	"github.com/NYTimes/logrotate"
	logrus "github.com/sirupsen/logrus"
	"gopkg.in/alexcesaro/statsd.v2"
)

// Console flags
var (
	listen                = flag.String("l", ":8888", "port to accept requests")
	targetProduction      = flag.String("a", "localhost:8080", "where production traffic goes. http://localhost:8080/production")
	altTarget             = flag.String("b", "localhost:8081", "where testing traffic goes. response are skipped. http://localhost:8081/test")
	debug                 = flag.Bool("debug", false, "more logging, showing ignored output")
	productionTimeout     = flag.Int("a.timeout", 3, "timeout in seconds for production traffic")
	alternateTimeout      = flag.Int("b.timeout", 1, "timeout in seconds for alternate site traffic")
	productionHostRewrite = flag.Bool("a.rewrite", false, "rewrite the host header when proxying production traffic")
	alternateHostRewrite  = flag.Bool("b.rewrite", false, "rewrite the host header when proxying alternate site traffic")
	percent               = flag.Float64("p", 100.0, "float64 percentage of traffic to send to testing")
	tlsPrivateKey         = flag.String("key.file", "", "path to the TLS private key file")
	tlsCertificate        = flag.String("cert.file", "", "path to the TLS certificate file")
	logFile               = flag.String("log.file", "/tmp/teeproxy.log", "path to the log file")
	serviceName           = flag.String("service.name", "unknown", "service name")
	hostname              = flag.String("hostname", "localhost", "hostname")
	statsdAddress         = flag.String("statsd.address", "localhost:8125", "statsd server address in the for host:port")
	statsdPrefix          = flag.String("statsd.prefix", "teeproxy", "prefix to be used for metrics sent to statsd")
)

func main() {
	flag.Parse()

	runtime.GOMAXPROCS(runtime.NumCPU())

	var listener net.Listener

	//set up logging
	fileHandle, err := logrotate.NewFile(*logFile)
	if err != nil {
		log.Fatal(err)
	}
	defer fileHandle.Close()

	logrus.SetFormatter(&logrus.JSONFormatter{})
	logrus.SetOutput(fileHandle)

	logger := logrus.WithFields(logrus.Fields{
		"service_name": *serviceName,
		"hostname":     *hostname,
	})

	//set up statsd client
	log.Printf("%s, %s", statsd.Address(*statsdAddress), *statsdPrefix) //statsd.Address(*statsdAddress),
	statsdClient, err := statsd.New(statsd.Prefix(*statsdPrefix))       // Connect to the UDP port 8125 by default.
	if err != nil {
		// If nothing is listening on the target port, an error is returned and
		// the returned client does nothing but is still usable. So we can
		// just log the error and go on.
		log.Print(err)
	}
	defer statsdClient.Close()
	//log.Printf("%+v", statsdClient)
	httpStats := statsdClient.Clone(statsd.Prefix("http.in"))
	httpStatsPri := statsdClient.Clone(statsd.Prefix("http.pri"))
	httpStatsAlt := statsdClient.Clone(statsd.Prefix("http.alt"))

	logger.Info("Starting teeproxy...")

	//ssl stuff
	if len(*tlsPrivateKey) > 0 {
		cer, err := tls.LoadX509KeyPair(*tlsCertificate, *tlsPrivateKey)
		if err != nil {
			logger.Errorf("Failed to load certficate: %s and private key: %s", *tlsCertificate, *tlsPrivateKey)
			return
		}

		config := &tls.Config{Certificates: []tls.Certificate{cer}}
		listener, err = tls.Listen("tcp", *listen, config)
		if err != nil {
			logger.Errorf("Failed to listen to %s: %s", *listen, err)
			return
		}
	} else {
		listener, err = net.Listen("tcp", *listen)
		if err != nil {
			logger.Errorf("Failed to listen to %s: %s", *listen, err)
			return
		}
	}

	//prepare the handler
	h := proxy.Handler{
		Target:                *targetProduction,
		Alternative:           *altTarget,
		Randomizer:            *rand.New(rand.NewSource(time.Now().UnixNano())),
		Logger:                logger,
		HttpStats:             httpStats,
		HttpStatsPri:          httpStatsPri,
		HttpStatsAlt:          httpStatsAlt,
		Debug:                 *debug,
		AlternateTimeout:      *alternateTimeout,
		AlternateHostRewrite:  *alternateHostRewrite,
		Percent:               *percent,
		ProductionTimeout:     *productionTimeout,
		ProductionHostRewrite: *productionHostRewrite,
	}

	logger.Info("Ready to serve")
	http.Serve(listener, h)
}
