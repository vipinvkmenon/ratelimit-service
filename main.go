package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
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
	DEFAULT_LIMIT    = 10
	DEFAULT_DURATION = 0
)

var (
	limit       int
	rateLimiter *RateLimiter
	delay       int
)

func main() {
	log.SetOutput(os.Stdout)

	limit = getEnv("RATE_LIMIT", DEFAULT_LIMIT)
	delay = getEnv("DURATION", DEFAULT_LIMIT)
	log.Printf("limit per sec [%d]\n", limit)
	log.Printf("Set Delay [%d] milliseconds\n", delay)

	rateLimiter = NewRateLimiter(limit)

	http.HandleFunc("/stats", statsHandler)
	http.Handle("/", newProxy())
	// To change ratelimit and delays on the fly
	http.HandleFunc("/config", onTheFlyConfig)
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
	delayInMilliseconds(delay)

	return res, err
}

func delayInMilliseconds(duration int) {
	log.Printf("Adding Delay of [%d] milliseconds to the request", duration)
	time.Sleep(time.Duration(duration) * time.Millisecond)
}

func onTheFlyConfig(w http.ResponseWriter, r *http.Request) {

	var oldDelay = delay
	var oldLimit = limit

	delayVal := r.URL.Query().Get("DELAY")
	rateLimitVal := r.URL.Query().Get("LIMIT")

	if delayVal != "" {
		newDelay, err := strconv.Atoi(delayVal)
		if err != nil {

			log.Printf("Invalid delay value, setting to default")
			delay = oldDelay
		} else {
			log.Printf("Setting Delay: [%d] milliseconds ", newDelay)
			delay = newDelay

		}
	}
	if rateLimitVal != "" {
		newLimit, err := strconv.Atoi(rateLimitVal)
		if err != nil {
			log.Printf("Invalid Limit value, setting to default")
			limit = oldLimit
		} else {
			log.Printf("Setting Rate Limit Value : [%d]", newLimit)

			delay = newLimit
		}
	}
}
