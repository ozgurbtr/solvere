package solvere

import (
	"context"
	"errors"
	"time"

	"github.com/miekg/dns"
)

var (
	ErrNoDNSKEY               = errors.New("solvere: No DNSKEY records found")
	ErrMissingKSK             = errors.New("solvere: No KSK DNSKEY found for DS records")
	ErrFailedToConvertKSK     = errors.New("solvere: Failed to convert KSK DNSKEY record to DS record")
	ErrMismatchingDS          = errors.New("solvere: KSK DNSKEY record does not match DS record from parent zone")
	ErrNoSignatures           = errors.New("solvere: No RRSIG records for zone that should be signed")
	ErrMissingDNSKEY          = errors.New("solvere: No matching DNSKEY found for RRSIG records")
	ErrInvalidSignaturePeriod = errors.New("solvere: Incorrect signature validity period")
	ErrBadAnswer              = errors.New("solvere: Response contained a non-zero RCODE")
	ErrMissingSigned          = errors.New("solvere: Signed records are missing")
)

func (rr *RecursiveResolver) lookupDNSKEY(ctx context.Context, auth *Nameserver) (map[uint16]*dns.DNSKEY, *LookupLog, func(), error) {
	q := &Question{Name: auth.Zone, Type: dns.TypeDNSKEY}
	var r *dns.Msg
	var log *LookupLog
	var err error
	if rr.cache != nil {

	}
	if rr.cache != nil {
		if a := rr.cache.Get(q); a != nil {
			r = new(dns.Msg)
			r.Rcode = dns.RcodeSuccess
			r.Answer = a.Answer
			r.Ns = a.Authority
			r.Extra = a.Additional
			log = newLookupLog(q, nil)
			log.CacheHit = true
			log.DNSSECValid = a.Authenticated
			log.Rcode = dns.RcodeSuccess
		}
	}
	if r == nil {
		r, log, err = rr.query(ctx, q, auth)
		if err != nil {
			return nil, log, nil, err
		}

		if len(r.Answer) == 0 || r.Rcode != dns.RcodeSuccess {
			return nil, log, nil, ErrNoDNSKEY
		}
	}

	keyMap := make(map[uint16]*dns.DNSKEY)
	// Extract DNSKEYs based on type
	for _, a := range r.Answer {
		if a.Header().Rrtype == dns.TypeDNSKEY {
			dnskey := a.(*dns.DNSKEY)
			tag := dnskey.KeyTag()
			if dnskey.Flags == 256 || dnskey.Flags == 257 {
				keyMap[tag] = dnskey
			}
		}
	}

	if len(keyMap) == 0 {
		return nil, log, nil, ErrNoDNSKEY // ???
	}

	// Verify RRSIGs from the message passed in using the KSK keys
	if auth.Zone != "." {
		err = verifyRRSIG(r, keyMap)
		if err != nil {
			return nil, log, nil, err
		}
	}

	addCache := func() {
		if rr.cache != nil && !log.CacheHit {
			rr.cache.Add(q, &Answer{r.Answer, r.Ns, r.Extra, dns.RcodeSuccess, true}, false)
		}
	}

	return keyMap, log, addCache, nil
}

func checkDS(keyMap map[uint16]*dns.DNSKEY, parentDSSet []dns.RR) error {
	for _, r := range parentDSSet {
		parentDS := r.(*dns.DS)
		// This KSK may not actually be of the right type but that
		// doesn't really matter since it'll serve the same purpose
		// either way if we find it in the map.
		ksk, present := keyMap[parentDS.KeyTag]
		if !present {
			continue
		}
		ds := ksk.ToDS(parentDS.DigestType)
		if ds == nil {
			return ErrFailedToConvertKSK
		}
		if ds.Digest != parentDS.Digest {
			return ErrMismatchingDS
		}
		return nil
	}
	return ErrMissingKSK
}

func verifyRRSIG(msg *dns.Msg, keyMap map[uint16]*dns.DNSKEY) error {
	for _, section := range [][]dns.RR{msg.Answer, msg.Ns} {
		if len(section) == 0 {
			continue
		}
		sigs := extractRRSet(section, "", dns.TypeRRSIG)
		if len(sigs) == 0 {
			return ErrNoSignatures
		}
		for _, sigRR := range sigs {
			sig := sigRR.(*dns.RRSIG)
			rest := extractRRSet(section, sig.Header().Name, sig.TypeCovered)
			if len(rest) == 0 {
				return ErrMissingSigned
			}
			k, present := keyMap[sig.KeyTag]
			if !present {
				return ErrMissingDNSKEY
			}
			err := sig.Verify(k, rest)
			if err != nil {
				return err
			}
			if !sig.ValidityPeriod(time.Time{}) {
				return ErrInvalidSignaturePeriod
			}
		}
	}
	return nil
}

func (rr *RecursiveResolver) checkSignatures(ctx context.Context, m *dns.Msg, auth *Nameserver, parentDSSet []dns.RR) (*LookupLog, error) {
	keyMap, log, addCache, err := rr.lookupDNSKEY(ctx, auth)
	if err != nil {
		return log, err
	}

	if len(parentDSSet) > 0 {
		err = checkDS(keyMap, parentDSSet)
		if err != nil {
			return log, err
		}
	}

	err = verifyRRSIG(m, keyMap)
	if err != nil {
		return log, err
	}

	log.DNSSECValid = true

	// Only add response to cache if it wasn't a cache hit
	if !log.CacheHit {
		if rr.cache != nil {
			addCache()
		}
	}

	return log, nil
}
