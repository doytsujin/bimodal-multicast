/*
Copyright 2019 Robert Andrei STEFAN

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/rstefan1/bimodal-multicast/pkg/internal/buffer"
	"github.com/rstefan1/bimodal-multicast/pkg/internal/callback"
	"github.com/rstefan1/bimodal-multicast/pkg/internal/httputil"
	"github.com/rstefan1/bimodal-multicast/pkg/internal/peer"
	"github.com/rstefan1/bimodal-multicast/pkg/internal/round"
)

type Server struct {
	server            *http.Server
	peerBuffer        *peer.PeerBuffer
	msgBuffer         *buffer.MessageBuffer
	gossipRoundNumber *round.GossipRound
	logger            *log.Logger
}

// runCustomCallbacks runs custom callbacks
func runCustomCallbacks(m buffer.Message, cfg Config, hostAddr, hostPort string) {
	// get callback from callbacks registry
	callbackFn, err := cfg.CustomCallbacks.GetCustomCallback(m.CallbackType)
	if err != nil {
		return
	}

	// run callback function
	ok, err := callbackFn(m.Msg, cfg.Logger)
	if err != nil {
		cfg.Logger.Printf("BMMC %s:%s: Error at calling callback function: %s", hostAddr, hostPort, err)
	}

	// add message in buffer only if callback call returns true
	if ok {
		cfg.MsgBuf.AddMessage(m)
		cfg.Logger.Printf("BMMC %s:%s synced buffer with message %s in round %d", hostAddr, hostPort, m.ID, cfg.GossipRound.GetNumber())
	}
}

// runDefaultCallbacks runs default callbacks
func runDefaultCallbacks(m buffer.Message, cfg Config, hostAddr, hostPort string) {
	// get callback from callbacks registry
	callbackFn, err := cfg.DefaultCallbacks.GetDefaultCallback(m.CallbackType)
	if err != nil {
		return
	}

	// TODO find a way to remove the following switch
	var p interface{}
	switch m.CallbackType {
	case callback.ADDPEER:
		p = cfg.PeerBuf
	case callback.REMOVEPEER:
		p = cfg.PeerBuf
	default:
		p = nil
	}

	// run callback function
	ok, err := callbackFn(m, p, cfg.Logger)
	if err != nil {
		cfg.Logger.Printf("BMMC %s:%s: Error at calling callback function: %s", hostAddr, hostPort, err)
	}

	// add message in buffer only if callback call returns true
	if ok {
		cfg.MsgBuf.AddMessage(m)
		cfg.Logger.Printf("BMMC %s:%s synced buffer with message %s in round %d", hostAddr, hostPort, m.ID, cfg.GossipRound.GetNumber())
	}
}

func gossipHandler(w http.ResponseWriter, r *http.Request, cfg Config) {
	gossipDigestBuffer, tAddr, tPort, tRoundNumber, err := httputil.ReceiveGossip(r)
	if err != nil {
		cfg.Logger.Printf("%s", err)
		return
	}

	msgDigestBuffer := cfg.MsgBuf.DigestBuffer()
	missingDigestBuffer := gossipDigestBuffer.GetMissingDigests(msgDigestBuffer)

	host := strings.Split(r.Host, ":")
	hostAddr := host[0]
	hostPort := host[1]

	if missingDigestBuffer.Length() > 0 {
		solicitationMsg := httputil.HTTPSolicitation{
			Addr:        hostAddr,
			Port:        hostPort,
			RoundNumber: tRoundNumber,
			Digests:     *missingDigestBuffer,
		}

		err = httputil.SendSolicitation(solicitationMsg, tAddr, tPort)
		if err != nil {
			cfg.Logger.Printf("%s", err)
			return
		}
	}
}

func solicitationHandler(w http.ResponseWriter, r *http.Request, cfg Config) {
	missingDigestBuffer, tAddr, tPort, _, err := httputil.ReceiveSolicitation(r)
	if err != nil {
		cfg.Logger.Printf("%s", err)
		return
	}
	missingMsgBuffer := missingDigestBuffer.GetMissingMessageBuffer(cfg.MsgBuf)

	host := strings.Split(r.Host, ":")
	hostAddr := host[0]
	hostPort := host[1]

	synchronizationMsg := httputil.HTTPSynchronization{
		Addr:     hostAddr,
		Port:     hostPort,
		Messages: *missingMsgBuffer,
	}

	err = httputil.SendSynchronization(synchronizationMsg, tAddr, tPort)
	if err != nil {
		cfg.Logger.Printf("%s", err)
		return
	}
}

func synchronizationHandler(w http.ResponseWriter, r *http.Request, cfg Config) {
	host := strings.Split(r.Host, ":")
	hostAddr := host[0]
	hostPort := host[1]

	rcvMsgBuf, _, _, err := httputil.ReceiveSynchronization(r)
	if err != nil {
		cfg.Logger.Printf("BMMC %s:%s Error at receiving synchronization error: %s", hostAddr, hostPort, err)
		return
	}

	for _, m := range rcvMsgBuf.Messages {
		// run callback function for messages with a callback registered
		if m.CallbackType != callback.NOCALLBACK {
			runDefaultCallbacks(m, cfg, hostAddr, hostPort)
			runCustomCallbacks(m, cfg, hostAddr, hostPort)
		} else {
			cfg.MsgBuf.AddMessage(m)
			cfg.Logger.Printf("BMMC %s:%s synced buffer with message %s in round %d", hostAddr, hostPort, m.ID, cfg.GossipRound.GetNumber())
		}
	}
}

func startServer(s *Server) error {
	s.logger.Printf("Server listening at %s", s.server.Addr)
	if err := s.server.ListenAndServe(); err != http.ErrServerClosed {
		s.logger.Printf("Unable to start  server: %s", err)
		return err
	}
	return nil
}

func gracefullyShutdown(s *Server) {
	if err := s.server.Shutdown(context.TODO()); err != nil {
		s.logger.Printf("Unable to shutdown server properly: %s", err)
	}
}

func New(cfg Config) *Server {
	if cfg.Logger == nil {
		cfg.Logger = log.New(os.Stdout, "", 0)
	}

	return &Server{
		server: &http.Server{
			Addr: fmt.Sprintf("%s:%s", cfg.Addr, cfg.Port),
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch path := r.URL.Path; path {
				case "/gossip":
					gossipHandler(w, r, cfg)
				case "/solicitation":
					solicitationHandler(w, r, cfg)
				case "/synchronization":
					synchronizationHandler(w, r, cfg)
				}
			}),
		},
		peerBuffer:        cfg.PeerBuf,
		msgBuffer:         cfg.MsgBuf,
		gossipRoundNumber: cfg.GossipRound,
		logger:            cfg.Logger,
	}
}

func (s *Server) Start(stop <-chan struct{}) error {
	errChan := make(chan error)

	go func() {
		err := startServer(s)
		errChan <- err
	}()

	go func() {
		<-stop
		gracefullyShutdown(s)
	}()

	// return <-errChan
	return nil
}
