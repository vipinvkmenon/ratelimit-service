package main

import (
	"fmt"
	"log"
	"time"

	"github.com/vipinvkmenon/ratelimit-service/store"
)

type Stats []Stat
type Stat struct {
	Ip        string `json:"ip"`
	Available int    `json:"available"`
}

type RateLimiter struct {
	duration time.Duration
	store    store.Store
}

func NewRateLimiter(limit int) *RateLimiter {

	return &RateLimiter{
		store: store.NewStore(limit),
	}
}

func (r *RateLimiter) ExceedsLimit(ip string) bool {
	if _, err := r.store.Increment(ip); err != nil {
		fmt.Printf("rate limit exceeded for %s\n", ip)
		return true
	}

	return false
}

func (r *RateLimiter) GetStats() Stats {
	s := Stats{}
	for k, v := range r.store.Stats() {
		s = append(s, Stat{
			Ip:        k,
			Available: v,
		})
	}
	return s
}

func (r *RateLimiter) AbovePercentage(ip string, limit int, percentage int) bool {
	totalAvailable := float64(r.store.GetAvailable(ip))
	log.Printf("Total Available %f", totalAvailable)
	log.Printf("Limit %d", limit)

	availablePercent := totalAvailable / float64(limit) * 100
	log.Printf(" Available Percent %f", availablePercent)
	if availablePercent >= float64(percentage) {
		return true
	}
	log.Printf(" Below limit")
	return false

}
