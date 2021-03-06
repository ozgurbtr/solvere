package solvere

import (
	"crypto/sha1"
	"net"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/jmhodges/clock"
)

func TestMinTTL(t *testing.T) {
	rrSet := []dns.RR{
		&dns.A{Hdr: dns.RR_Header{Ttl: 2}},
		&dns.A{Hdr: dns.RR_Header{Ttl: 5}},
		&dns.A{Hdr: dns.RR_Header{Ttl: 1}},
	}
	min := minTTL(rrSet, clock.Default())
	if min != 1 {
		t.Fatalf("minTTL produced the wrong TTL: expected %d, got %d", 1, min)
	}
	if minTTL([]dns.RR{}, clock.Default()) != 0 {
		t.Fatalf("minTTL produced a non-zero TTL with a empty RR set")
	}

	fc := clock.NewFake()
	n := time.Now().Add(time.Second).UTC().Unix()
	mod := (n / year68) - 1
	if mod < 0 {
		mod = 0
	}
	e := uint32(n - (mod * year68))
	rrSet = []dns.RR{
		&dns.A{Hdr: dns.RR_Header{Ttl: 5}},
		&dns.RRSIG{Hdr: dns.RR_Header{Ttl: 4, Rrtype: dns.TypeRRSIG}, Expiration: e},
	}
	min = minTTL(rrSet, fc)
	if min != 1 {
		t.Fatalf("minTTL didn't account for RRSIG expiring before TTL: wanted %d, got %d", 1, min)
	}
}

func TestCache(t *testing.T) {
	fc := clock.NewFake()
	cache := &BasicCache{cache: make(map[[sha1.Size]byte]*cacheEntry), clk: fc}

	q := Question{Name: "testing", Type: dns.TypeA}
	ca := cache.Get(&q)
	if ca != nil {
		t.Fatalf("Empty answer returned non-nil Answer: %#v", ca)
	}

	a := Answer{Answer: []dns.RR{&dns.A{Hdr: dns.RR_Header{Ttl: 5}, A: net.IP{1, 2, 3, 4}}}}
	cache.Add(&q, &a, true)
	ca = cache.Get(&q)
	if ca != &a {
		t.Fatalf("Cache returned incorrect answer: expected %#v, got %#v", a, ca)
	}
	fc.Add(time.Second * 30)
	cache.fullPrune()
	ca = cache.Get(&q)
	if ca == nil {
		t.Fatal("Cache pruned q/a that should've been kept forever")
	}

	q = Question{Name: "testing-2", Type: dns.TypeA}
	cache.Add(&q, &a, false)
	ca = cache.Get(&q)
	if ca != &a {
		t.Fatalf("Cache returned incorrect answer: expected %#v, got %#v", a, ca)
	}
	fc.Add(time.Second * 30)
	cache.fullPrune()
	ca = cache.Get(&q)
	if ca != nil {
		t.Fatal("Cache didn't prune q/a that had a minimum TTL of 5 seconds after 30 seconds")
	}

	na := Answer{Answer: []dns.RR{&dns.A{Hdr: dns.RR_Header{Ttl: 2}, A: net.IP{1, 2, 3, 5}}}}
	cache.Add(&q, &a, false)
	fc.Add(time.Second * 2)
	cache.Add(&q, &na, false)
	fc.Add(time.Second * 3)
	cache.fullPrune()
	ca = cache.Get(&q)
	if ca != nil {
		t.Fatal("Cache didn't prune q/a that had a minimum TTL of 2 second")
	}

	a = Answer{Answer: []dns.RR{&dns.A{Hdr: dns.RR_Header{}, A: net.IP{1, 2, 3, 4}}}}
	cache.Add(&q, &a, false)
	ca = cache.Get(&q)
	if ca != nil {
		t.Fatalf("Answer with 0 minTTL stored/returned: %#v", ca)
	}

	a = Answer{Answer: []dns.RR{&dns.A{Hdr: dns.RR_Header{Ttl: 10}, A: net.IP{1, 2, 3, 4}}}}
	cache.Add(&q, &a, false)
	fc.Add(time.Second * 20)
	ca = cache.Get(&q)
	if ca != nil {
		t.Fatalf("Answer with expired TTL returned: %#v", ca)
	}

}
