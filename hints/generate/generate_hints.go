package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/rolandshoemaker/dns" // revert to miekg when tokenUpper PR lands
)

type dk struct {
	TTL       uint32
	Flags     uint16
	Protocol  uint8
	Algorithm uint8
	PublicKey string
}

type ns struct {
	StructType string
	Name       string
	Rrtype     uint16
	TTL        uint32
	Ns         string
	A          string
	AAAA       string
}

type content struct {
	Generated       time.Time
	RootKeys        []dk
	RootNameservers []ns
}

const tmpl = ` // Package hints provides the DNSKEY and NS/A/AAAA records for the root DNS zone
package hints

// generated by hints/generate/generate_hints.go at {{.Generated}}
// DO NOT EDIT BY HAND

import (
  "net"

	"github.com/rolandshoemaker/dns" // revert to miekg when tokenUpper PR lands
)

// RootKeys contains the DNSKEY records for the root zone
var RootKeys = []dns.RR{
{{range .RootKeys}}
  &dns.DNSKEY{
    Hdr: dns.RR_Header{
      Name: ".",
       Rrtype: 48,
       Class: 1,
       Ttl: {{.TTL}},
    },
    Flags: {{.Flags}},
    Protocol: {{.Protocol}},
    Algorithm: {{.Algorithm}},
    PublicKey: "{{.PublicKey}}",
  },
{{end}}
}

// RootNameservers contains the NS and A/AAAA records for the root zone nameservers
var RootNameservers = []dns.RR{
{{range .RootNameservers}}
	&dns.{{.StructType}}{
    Hdr: dns.RR_Header{
      Name: "{{.Name}}",
       Rrtype: {{.Rrtype}},
       Class: 1,
       Ttl: {{.TTL}},
    },
    {{if .Ns}}
    Ns: "{{.Ns}}",
    {{else if .A}}
    A: net.IP{ {{.A}} },
    {{else if .AAAA}}
    AAAA: net.IP{ {{.AAAA}} },
    {{end}}
  },
{{end}}
}
`

func bytesToString(bytes []byte) string {
	intStrs := []string{}
	for _, b := range bytes {
		intStrs = append(intStrs, strconv.Itoa(int(b)))
	}
	return strings.Join(intStrs, ",")
}

func main() {
	keyFile := flag.String("rootKeys", "", "")
	nsFile := flag.String("rootNameservers", "", "")
	output := flag.String("output", "", "")
	flag.Parse()
	if *keyFile == "" || *nsFile == "" || *output == "" {
		fmt.Fprintln(os.Stderr, "-rootKeys, -rootNameservers, and -output are all required")
		os.Exit(1)
	}

	kf, err := os.Open(*keyFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open key file %q: %s\n", *keyFile, err)
		os.Exit(1)
	}
	defer kf.Close()
	nsf, err := os.Open(*nsFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open nameservers file %q: %s\n", *nsFile, err)
		os.Exit(1)
	}
	defer nsf.Close()
	o, err := os.OpenFile(*output, os.O_RDWR|os.O_CREATE|os.O_TRUNC, os.ModePerm)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open output file %q: %s\n", *output, err)
		os.Exit(1)
	}
	defer o.Close()

	c := content{Generated: time.Now()}

	tokens := dns.ParseZone(kf, "", "")
	for x := range tokens {
		if x.Error != nil {
			fmt.Fprintf(os.Stderr, "Failed to parse records from key file: %s\n", err)
			os.Exit(1)
		}
		d := x.RR.(*dns.DNSKEY)
		c.RootKeys = append(c.RootKeys, dk{
			TTL:       x.RR.Header().Ttl,
			Flags:     d.Flags,
			Protocol:  d.Protocol,
			Algorithm: d.Algorithm,
			PublicKey: d.PublicKey,
		})
	}
	tokens = dns.ParseZone(nsf, "", "")
	for x := range tokens {
		if x.Error != nil {
			fmt.Fprintf(os.Stderr, "Failed to parse records from nameservers file: %s\n", err)
			os.Exit(1)
		}
		rns := ns{
			Name:   x.RR.Header().Name,
			Rrtype: x.RR.Header().Rrtype,
			TTL:    x.RR.Header().Ttl,
		}
		switch n := x.RR.(type) {
		case *dns.NS:
			rns.StructType = "NS"
			rns.Ns = n.Ns
		case *dns.A:
			rns.StructType = "A"
			rns.A = bytesToString(n.A[12:])
		case *dns.AAAA:
			rns.StructType = "AAAA"
			rns.AAAA = bytesToString(n.AAAA)
		}
		c.RootNameservers = append(c.RootNameservers, rns)
	}

	t := template.Must(template.New("hints").Parse(tmpl))

	err = t.Execute(o, c)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to render and write code template: %s\n", err)
		os.Exit(1)
	}

	cmd := exec.Command("gofmt", "-w", *output)
	cmd.Env = os.Environ()
	err = cmd.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to format %q with gofmt: %s\n", *output, err)
		os.Exit(1)
	}
}
