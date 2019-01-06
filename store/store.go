package store

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/juju/ratelimit"
)

const expireInSecs = 30 * time.Second

type Store interface {
	Increment(string) (int, error)
	Stats() map[string]int
}

type InMemoryStore struct {
	limit    int
	duration int
	storage  map[string]*entry
	sync.RWMutex
}

type entry struct {
	bucket    *ratelimit.Bucket
	updatedAt time.Time
}

func (e *entry) Expired() bool {
	return time.Now().After(e.updatedAt.Add(expireInSecs))
}

func NewStore(limit int) Store {
	store := &InMemoryStore{
		limit:    limit,
		duration: -1,
		storage:  make(map[string]*entry),
	}
	store.expiryCycle()

	return store
}

func NewStoreWithDuration(limit int, duration int) Store {
	store := &InMemoryStore{
		limit:    limit,
		duration: duration,
		storage:  make(map[string]*entry),
	}
	store.expiryCycle()

	return store
}

func newEntry(limit int) *entry {
	fillRatePerSec := 1000 / limit

	// Logic check -> any value lesser than 10 milliseconds probably would not make this ratelimiter effective...so lets set hard limit of 10 milliseconds
	// probably this duration should be based on the RTT value of a single request....
	return &entry{
		bucket: ratelimit.NewBucket(time.Duration(fillRatePerSec)*time.Millisecond, int64(limit)),
	}
}
func newEntryWithDuration(limit int, duration int) *entry {
	fillRatePerSec := duration

	// Logic check -> any value lesser than 10 milliseconds probably would not make this ratelimiter effective...so lets set hard limit of 10 milliseconds
	// probably this duration should be based on the RTT value of a single request....
	return &entry{
		bucket: ratelimit.NewBucket(time.Duration(fillRatePerSec)*time.Millisecond, int64(limit)),
	}
}

func (s *InMemoryStore) Increment(key string) (int, error) {
	v, ok := s.get(key)
	if !ok {
		if s.duration == -1 {
			v = newEntry(s.limit)
		} else {
			v = newEntryWithDuration(s.limit, s.duration)
		}

	}
	if avail := v.bucket.Available(); avail == 0 {
		v.updatedAt = time.Now()
		s.set(key, v)
		return int(avail), errors.New("empty bucket")
	}
	v.bucket.Take(1)
	v.updatedAt = time.Now()
	s.set(key, v)
	return int(v.bucket.Available()), nil
}

func (s *InMemoryStore) get(key string) (*entry, bool) {
	s.RLock()
	defer s.RUnlock()
	v, ok := s.storage[key]
	return v, ok
}

func (s *InMemoryStore) set(key string, value *entry) {
	s.Lock()
	defer s.Unlock()
	s.storage[key] = value
}

func (s *InMemoryStore) expiryCycle() {
	ticker := time.NewTicker(time.Millisecond * 500)
	go func() {
		for _ = range ticker.C {
			s.Lock()
			for k, v := range s.storage {
				if v.Expired() {
					fmt.Printf("removing expired key [%s]\n", k)
					delete(s.storage, k)
				}
			}
			s.Unlock()
		}
	}()
}

func (s *InMemoryStore) Available(key string) int {
	v, ok := s.get(key)
	if !ok {
		return 0
	}
	return int(v.bucket.Available())
}

func (s *InMemoryStore) Stats() map[string]int {
	m := make(map[string]int)
	s.Lock()
	for k, v := range s.storage {
		m[k] = int(v.bucket.Available())
	}
	s.Unlock()
	return m
}
