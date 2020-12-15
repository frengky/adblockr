package adblockr

import (
	"github.com/miekg/dns"
	"github.com/patrickmn/go-cache"
	log "github.com/sirupsen/logrus"
	"net"
	"sync"
	"time"
)

const (
	notIPQuery = 0
	_IP4Query  = 4
	_IP6Query  = 6
)

var (
	RejectWithNXDomain        = false
	RejectTTL          uint32 = 3600
	NullRoute                 = "0.0.0.0"
	NullRouteV6               = "0:0:0:0:0:0:0:0"
)

type dnsRequest struct {
	network string
	w       dns.ResponseWriter
	r       *dns.Msg
}

type Server struct {
	address         string
	readTimeout     time.Duration
	writeTimeout    time.Duration
	requestChan     chan dnsRequest
	quit            chan struct{}
	blacklist       DomainBucket
	whitelist       DomainBucket
	resolver        Resolver
	tcpServer       *dns.Server
	udpServer       *dns.Server
	cache           *cache.Cache
	cacheExpire     time.Duration
	cleanUpInterval time.Duration
}

func NewServer(address string, resolver Resolver, blacklist DomainBucket, whitelist DomainBucket,
	cacheExpire time.Duration, cleanUpInterval time.Duration) *Server {
	timeout := 3 * time.Second

	srv := &Server{
		address:      address,
		readTimeout:  timeout,
		writeTimeout: timeout,
		requestChan:  make(chan dnsRequest),
		quit:         make(chan struct{}, 1),
		resolver:     resolver,
		blacklist:    blacklist,
		whitelist:    whitelist,
		cache:        cache.New(cacheExpire, cleanUpInterval),
	}

	return srv
}

func (s *Server) ListenAndServe() {
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		s.handleRequest()
	}()

	tcpHandler := dns.NewServeMux()
	tcpHandler.HandleFunc(".", s.handleTCP)
	s.tcpServer = &dns.Server{
		Addr:         s.address,
		Net:          "tcp",
		Handler:      tcpHandler,
		ReadTimeout:  s.readTimeout,
		WriteTimeout: s.writeTimeout,
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := s.tcpServer.ListenAndServe(); err != nil {
			log.WithError(err).Error("tcp server error")
		}
	}()

	udpHandler := dns.NewServeMux()
	udpHandler.HandleFunc(".", s.handleUDP)
	s.udpServer = &dns.Server{
		Addr:         s.address,
		Net:          "udp",
		Handler:      udpHandler,
		UDPSize:      65535,
		ReadTimeout:  s.readTimeout,
		WriteTimeout: s.writeTimeout,
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := s.udpServer.ListenAndServe(); err != nil {
			log.WithError(err).Error("udp server error")
		}
	}()

	log.WithField("listen", s.address).Info("ready for connection")
	wg.Wait()
	log.Info("stopped")
}

func (s *Server) Shutdown() {
	log.Debug("shutting down")

	_ = s.tcpServer.Shutdown()
	_ = s.udpServer.Shutdown()
	close(s.quit)
}

func (s *Server) handleRequest() {
loop:
	for {
		select {
		case <-s.quit:
			log.Debug("quit signal received")
			break loop
		case req, ok := <-s.requestChan:
			if !ok {
				break
			}
			func(network string, w dns.ResponseWriter, r *dns.Msg) {
				defer w.Close()
				q := r.Question[0]

				var clientIP string
				if network == "tcp" {
					clientIP = w.RemoteAddr().(*net.TCPAddr).IP.String()
				} else {
					clientIP = w.RemoteAddr().(*net.UDPAddr).IP.String()
				}

				qName := unFqdn(q.Name)
				qType := dns.TypeToString[q.Qtype]
				qClass := dns.ClassToString[q.Qclass]

				logCtx := log.WithFields(log.Fields{
					"net":       network,
					"client-ip": clientIP,
					"name":      q.Name,
					"type":      qType,
					"class":     qClass,
				})

				question := qName + " " + qType + " " + qClass
				c, found := s.cache.Get(question)
				if found {
					mc := c.(*dns.Msg)
					msg := mc
					msg.Id = r.Id
					s.writeReply(w, msg)
					return
				}

				var isWhitelisted = s.whitelist.Has(qName)
				var isBlacklisted = false

				ipQuery := isIPQuery(q)
				if ipQuery > 0 {

					if !isWhitelisted {
						isBlacklisted = s.blacklist.Has(qName)
					}

					if isBlacklisted {
						m := new(dns.Msg)
						m.SetReply(r)

						if RejectWithNXDomain {
							m.SetRcode(r, dns.RcodeNameError)
						} else {
							nullRoute := net.ParseIP(NullRoute)
							nullRouteV6 := net.ParseIP(NullRouteV6)

							switch ipQuery {
							case _IP4Query:
								rrHeader := dns.RR_Header{
									Name:   q.Name,
									Rrtype: dns.TypeA,
									Class:  dns.ClassINET,
									Ttl:    RejectTTL,
								}
								a := &dns.A{Hdr: rrHeader, A: nullRoute}
								m.Answer = append(m.Answer, a)
							case _IP6Query:
								rrHeader := dns.RR_Header{
									Name:   q.Name,
									Rrtype: dns.TypeAAAA,
									Class:  dns.ClassINET,
									Ttl:    RejectTTL,
								}
								a := &dns.AAAA{Hdr: rrHeader, AAAA: nullRouteV6}
								m.Answer = append(m.Answer, a)
							}
						}
						s.writeReply(w, m)
						logCtx.Warn("dns query rejected")
						s.cache.Add(question, m, s.cacheExpire)
						return
					}
				}

				result, err := s.resolver.Lookup(network, r)
				if err != nil {
					s.handleFailed(w, r)
					logCtx.WithError(err).Error("lookup failed")
					return
				}

				s.writeReply(w, result)
				logCtx.Debug("dns query success")

				var cacheTtl uint32 = 600
				for _, answer := range result.Answer {
					ttl := answer.Header().Ttl
					if ttl > 0 && ttl < cacheTtl {
						cacheTtl = ttl
					}
				}
				cacheDuration := time.Duration(cacheTtl) * time.Second
				if cacheDuration.Milliseconds() > s.cacheExpire.Milliseconds() {
					cacheDuration = s.cacheExpire
				}
				s.cache.Add(question, result, cacheDuration)

			}(req.network, req.w, req.r)
		}
	}
}

func (s *Server) handleTCP(w dns.ResponseWriter, r *dns.Msg) {
	s.requestChan <- dnsRequest{network: "tcp", w: w, r: r}
}

func (s *Server) handleUDP(w dns.ResponseWriter, r *dns.Msg) {
	s.requestChan <- dnsRequest{network: "udp", w: w, r: r}
}

func (s *Server) writeReply(w dns.ResponseWriter, r *dns.Msg) {
	defer func() {
		if r := recover(); r != nil {
			log.WithField("recover", r).Warn("recovered from replying")
		}
	}()
	if err := w.WriteMsg(r); err != nil {
		log.WithError(err).Error("error while replying")
	}
}

func (s *Server) handleFailed(w dns.ResponseWriter, r *dns.Msg) {
	m := new(dns.Msg)
	m.SetRcode(r, dns.RcodeServerFailure)
	s.writeReply(w, m)
}

func isIPQuery(q dns.Question) int {
	if q.Qclass != dns.ClassINET {
		return notIPQuery
	}
	switch q.Qtype {
	case dns.TypeA:
		return _IP4Query
	case dns.TypeAAAA:
		return _IP6Query
	default:
		return notIPQuery
	}
}

func unFqdn(s string) string {
	if dns.IsFqdn(s) {
		return s[:len(s)-1]
	}
	return s
}
