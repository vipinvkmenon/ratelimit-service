package main

import (
	"fmt"
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

func NewRateLimiterWithDuration(limit int, duration int) *RateLimiter {
	return &RateLimiter{
		store: store.NewStoreWithDuration(limit, duration),
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
