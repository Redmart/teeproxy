// handler
package handler

import (
	"bytes"
	"io"
	"io/ioutil"
	"math/rand"
	"net"
	"net/http"
	"net/http/httputil"
	"strconv"
	"sync"
	"time"

	logrus "github.com/sirupsen/logrus"
	"gopkg.in/alexcesaro/statsd.v2"
)

// handler contains the address of the main Target and the one for the Alternative target
type Handler struct {
	Target                string
	Alternative           string
	Randomizer            rand.Rand
	Logger                *logrus.Entry
	HttpStats             *statsd.Client
	HttpStatsPri          *statsd.Client
	HttpStatsAlt          *statsd.Client
	Debug                 bool
	AlternateTimeout      int
	AlternateHostRewrite  bool
	Percent               float64
	ProductionTimeout     int
	ProductionHostRewrite bool
}

var mutex = &sync.Mutex{}

func forwardToAlternate(h Handler, alternativeRequest *http.Request) {
	defer func() {
		if r := recover(); r != nil && h.Debug {
			h.Logger.Warn("Recovered in forwardToAlternate", r)
		}
	}()

	h.Logger.Infof("Forwarding request to alternate. Host: %s, URL: %s", h.Target, alternativeRequest.URL)
	t := h.HttpStatsAlt.NewTiming()
	// Open new TCP connection to the server
	clientTcpConn, err := net.DialTimeout("tcp", h.Alternative, time.Duration(time.Duration(h.AlternateTimeout)*time.Second))
	if err != nil {
		if h.Debug {
			h.Logger.Warnf("Failed to connect to alternate backend. Host: %s, URL: %s, Error: %v", h.Alternative, alternativeRequest.URL, err)
		}
		h.HttpStatsAlt.Increment(strconv.Itoa(http.StatusServiceUnavailable))
		return
	}

	clientHttpConn := httputil.NewClientConn(clientTcpConn, nil) // Start a new HTTP connection on it
	defer clientHttpConn.Close()                                 // Close the connection to the server
	if h.AlternateHostRewrite {
		alternativeRequest.Host = h.Alternative
	}

	err = clientHttpConn.Write(alternativeRequest) // Pass on the request
	if err != nil {
		if h.Debug {
			h.Logger.Warnf("Failed to send to alternate backend. Host: %s, URL: %s, Error: %v", h.Alternative, alternativeRequest.URL, err)
		}
		return
	}

	resp, err := clientHttpConn.Read(alternativeRequest) // Read back the reply
	if err != nil && err != httputil.ErrPersistEOF {
		if h.Debug {
			h.Logger.Warnf("Failed to receive from alternate backend. Host: %s, URL: %s, Error: %v", h.Alternative, alternativeRequest.URL, err)
		}
		return
	}
	defer resp.Body.Close()

	h.Logger.Infof("Response from alternate. Host: %s, Status: %d, URL: %s", h.Alternative, resp.StatusCode, alternativeRequest.URL)

	t.Send("latency")
	h.HttpStatsAlt.Increment(strconv.Itoa(resp.StatusCode))
}

// ServeHTTP duplicates the incoming request (req) and does the request to the Target
// and the Alternate target discading the Alternate response
func (h Handler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	var productionRequest, alternativeRequest *http.Request
	h.HttpStats.Increment("req")

	mutex.Lock()
	rnd := h.Randomizer.Float64()
	mutex.Unlock()

	if h.Percent == 100.0 || rnd*100 < h.Percent { //h.Randomizer
		alternativeRequest, productionRequest = DuplicateRequest(req)
		go forwardToAlternate(h, alternativeRequest)
	} else {
		productionRequest = req
	}

	defer func() {
		if r := recover(); r != nil && h.Debug {
			h.Logger.Info("Recovered in ServeHTTP", r)
		}
	}()

	h.Logger.Infof("Forwarding request to primary. Host: %s, URL: %s", h.Target, productionRequest.URL)
	t := h.HttpStatsPri.NewTiming()
	// Open new TCP connection to the server
	clientTcpConn, err := net.DialTimeout("tcp", h.Target, time.Duration(time.Duration(h.ProductionTimeout)*time.Second))
	if err != nil {
		h.Logger.Warnf("Failed to connect to primary backend. Host: %s, URL: %s, Error: %v", h.Target, productionRequest.URL, err)

		w.WriteHeader(http.StatusServiceUnavailable)
		h.HttpStatsPri.Increment(strconv.Itoa(http.StatusServiceUnavailable))
		return
	}

	clientHttpConn := httputil.NewClientConn(clientTcpConn, nil) // Start a new HTTP connection on it
	defer clientHttpConn.Close()                                 // Close the connection to the server

	if h.ProductionHostRewrite {
		productionRequest.Host = h.Target
	}

	err = clientHttpConn.Write(productionRequest) // Pass on the request
	if err != nil {
		h.Logger.Errorf("Failed to send to primary backend. Host: %s:, URL: %s, Error: %v", h.Target, productionRequest.URL, err)
		return
	}

	resp, err := clientHttpConn.Read(productionRequest) // Read back the reply
	if err != nil && err != httputil.ErrPersistEOF {
		h.Logger.Errorf("Failed to receive from primary backend. Host: %s:, URL: %s, Error: %v", h.Target, productionRequest.URL, err)
		return
	}

	defer resp.Body.Close()

	for k, v := range resp.Header {
		w.Header()[k] = v
	}

	w.WriteHeader(resp.StatusCode)
	body, _ := ioutil.ReadAll(resp.Body)
	w.Write(body)
	h.Logger.Infof("Response from primary. Host: %s, Status: %d, URL: %s", h.Target, resp.StatusCode, productionRequest.URL)

	t.Send("latency")
	h.HttpStatsPri.Increment(strconv.Itoa(resp.StatusCode))

}

func DuplicateRequest(request *http.Request) (request1 *http.Request, request2 *http.Request) {
	b2 := new(bytes.Buffer)
	b1 := new(bytes.Buffer)
	w := io.MultiWriter(b1, b2)
	io.Copy(w, request.Body)
	defer request.Body.Close()
	request1 = &http.Request{
		Method:        request.Method,
		URL:           request.URL,
		Proto:         request.Proto,
		ProtoMajor:    request.ProtoMajor,
		ProtoMinor:    request.ProtoMinor,
		Header:        request.Header,
		Body:          nopCloser{b1},
		Host:          request.Host,
		ContentLength: request.ContentLength,
		Close:         true,
	}
	request2 = &http.Request{
		Method:        request.Method,
		URL:           request.URL,
		Proto:         request.Proto,
		ProtoMajor:    request.ProtoMajor,
		ProtoMinor:    request.ProtoMinor,
		Header:        request.Header,
		Body:          nopCloser{b2},
		Host:          request.Host,
		ContentLength: request.ContentLength,
		Close:         true,
	}
	return
}

type nopCloser struct {
	io.Reader
}

func (nopCloser) Close() error { return nil }
