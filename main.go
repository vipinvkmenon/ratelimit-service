package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	DEFAULT_PORT     = "8080"
	CF_FORWARDED_URL = "X-Cf-Forwarded-Url"
	DEFAULT_LIMIT    = 10 //Rate Limit
	DEFAULT_DELAY    = 0  //Delay
	DEFAULT_DURATION = -1 //

	//The following headers are used by the cf router when the rate limiter uses the Fully Brokerd Plan
	//Refer https://docs.cloudfoundry.org/services/route-services.html
	CF_PROXY_SIGNATURE = "X-CF-Proxy-Signature"
	CF_PROXY_METADATA  = "X-CF-Proxy-Metadata"
)

var (
	limit       int
	rateLimiter *RateLimiter
	delay       int
	duration    int
)

func main() {
	log.SetOutput(os.Stdout)

	limit = getEnv("RATE_LIMIT", DEFAULT_LIMIT)
	delay = getEnv("DELAY", DEFAULT_DELAY)
	duration = getEnv("DURATION", DEFAULT_DURATION)
	log.Printf("limit per sec %d\n", limit)
	log.Printf("Set Delay %d milliseconds\n", delay)
	log.Printf("Set Duration %d milliseconds\n", duration)

	if duration == -1 {
		rateLimiter = NewRateLimiter(limit)
	} else {
		rateLimiter = NewRateLimiterWithDuration(limit, duration)
	}

	//Routes
	http.HandleFunc("/stats", statsHandler)
	http.Handle("/", newProxy())                       //Simple End point for RL service can be used with when using RL as CUPS
	http.Handle("/service-instance/", brokeredProxy()) //When using the RL as a brokered service
	http.HandleFunc("/config", onTheFlyConfig)         // To change ratelimit and delays on the fly
	log.Fatal(http.ListenAndServe(":"+getPort(), nil))
}

func newProxy() http.Handler {
	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			forwardedURL := req.Header.Get(CF_FORWARDED_URL)

			url, err := url.Parse(forwardedURL)
			if err != nil {
				log.Fatalln(err.Error())
			}

			req.URL = url
			req.Host = url.Host

		},
		Transport: newRateLimitedRoundTripper(),
	}
	return proxy
}

func statsHandler(w http.ResponseWriter, r *http.Request) {
	stats, err := json.Marshal(rateLimiter.GetStats())
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	fmt.Fprintf(w, string(stats))
}

func getPort() string {
	var port string
	if port = os.Getenv("PORT"); len(port) == 0 {
		port = DEFAULT_PORT
	}
	return port
}

func skipSslValidation() bool {
	var skipSslValidation bool
	var err error
	if skipSslValidation, err = strconv.ParseBool(os.Getenv("SKIP_SSL_VALIDATION")); err != nil {
		skipSslValidation = true
	}
	return skipSslValidation
}
func getEnv(env string, defaultValue int) int {
	var (
		v      string
		config int
	)
	if v = os.Getenv(env); len(v) == 0 {
		return defaultValue
	}

	config, err := strconv.Atoi(v)
	if err != nil {
		return defaultValue
	}
	return config
}

type RateLimitedRoundTripper struct {
	rateLimiter *RateLimiter
	transport   http.RoundTripper
}

func newRateLimitedRoundTripper() *RateLimitedRoundTripper {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: skipSslValidation()},
	}
	return &RateLimitedRoundTripper{
		rateLimiter: rateLimiter,
		transport:   tr,
	}
}

func (r *RateLimitedRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	var err error
	var res *http.Response

	remoteIP := strings.Split(req.RemoteAddr, ":")[0]

	log.Printf("request from [%s]\n", remoteIP)
	delayInMilliseconds(delay) //Since delays can be more than a second, add the delay before the request falls into the bucket.
	if r.rateLimiter.ExceedsLimit(remoteIP) {
		resp := &http.Response{
			StatusCode: 429,
			Body:       ioutil.NopCloser(bytes.NewBufferString("Too many requests")),
		}
		log.Printf("Too many requests")
		return resp, nil
	}

	res, err = r.transport.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	//DELAY Method
	//delayInMilliseconds(delay)

	return res, err
}

// Adds delay to processing the request
func delayInMilliseconds(duration int) {
	log.Printf("Adding Delay of [%d] milliseconds to the request", duration)
	time.Sleep(time.Duration(duration) * time.Millisecond)
}

//Simple API to change LIMIT and DELAY on demand
func onTheFlyConfig(w http.ResponseWriter, r *http.Request) {

	var oldDelay = delay
	var oldLimit = limit
	var oldDuration = duration

	delayVal := r.URL.Query().Get("DELAY")
	rateLimitVal := r.URL.Query().Get("LIMIT")
	durationVal := r.URL.Query().Get("DURATION")

	if delayVal != "" {
		newDelay, err := strconv.Atoi(delayVal)
		if err != nil {

			log.Printf("Invalid delay value, setting to default")
			delay = oldDelay
		} else {
			log.Printf("Setting Delay: [%d] milliseconds ", newDelay)
			delay = int(math.Abs(float64(newDelay)))

		}
	}
	if rateLimitVal != "" {
		newLimit, err := strconv.Atoi(rateLimitVal)
		if err != nil {
			log.Printf("Invalid Limit value, setting to default")
			limit = oldLimit
		} else {
			log.Printf("Setting Rate Limit Value : [%d]", newLimit)

			limit = int(math.Abs(float64(newLimit)))
			rateLimiter = NewRateLimiter(limit)
		}
	}
	if durationVal != "" {
		newDuration, err := strconv.Atoi(durationVal)
		if err != nil {
			log.Printf("Invalid Limit value, setting to default")
			duration = oldDuration
		} else {
			log.Printf("Setting Rate Limit Value : [%d]", newDuration)

			limit = int(math.Abs(float64(newDuration)))
			rateLimiter = NewRateLimiter(limit)
		}
	}

}

//Function to handle RL & Delay when using the service as a brokered service.
func brokeredProxy() http.Handler {
	log.Printf("Through Brokered Proxy")
	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {

			forwardedURL := req.Header.Get(CF_FORWARDED_URL)
			proxySignature := req.Header.Get(CF_PROXY_SIGNATURE)
			proxyMetadata := req.Header.Get(CF_PROXY_METADATA)
			//the request url will be in the format /service-instance/<ServiceInstanceID>/bind-instance/<BindInstanceID>
			//While currently not used for anythingg other than for logging this data can be used later
			path := req.URL.Path
			servInstance := strings.Split(path, "/")[2]
			log.Printf("Serv Instance " + servInstance)
			bindInstance := strings.Split(path, "/")[4]
			log.Printf("Bind Instance " + bindInstance)

			url, err := url.Parse(forwardedURL)
			if err != nil {
				log.Fatalln(err.Error())
			}

			req.URL = url
			req.Host = url.Host
			//As documented in the CF documentation these need to be added in the response header when using brokered approach
			req.Header.Set(CF_PROXY_SIGNATURE, proxySignature)
			req.Header.Set(CF_PROXY_METADATA, proxyMetadata)

		},
		Transport: newRateLimitedRoundTripper(),
	}
	return proxy
}
