package gossip

import (
	"fmt"
	"math/rand"
	"net"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/rstefan1/bimodal-multicast/pkg/httpserver"
	"github.com/rstefan1/bimodal-multicast/pkg/internal/buffer"
	"github.com/rstefan1/bimodal-multicast/pkg/internal/config"
	"github.com/rstefan1/bimodal-multicast/pkg/internal/peer"
)

const (
	timeout = time.Second
)

func suggestPort() int {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		panic(err)
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		panic(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

var _ = Describe("Gossip Server", func() {
	var (
		gossip       Gossip
		gossipPort   string
		mockPort     string
		gossipPeers  []peer.Peer
		mockPeers    []peer.Peer
		gossipMsgBuf buffer.MessageBuffer
		mockMsgBuf   buffer.MessageBuffer
		gossipCfg    config.GossipConfig
		httpCfg      config.HTTPConfig
		mockCfg      config.HTTPConfig
		gossipStop   chan struct{}
		httpStop     chan struct{}
		mockStop     chan struct{}
	)

	BeforeEach(func() {
		gossipPort = strconv.Itoa(suggestPort())
		mockPort = strconv.Itoa(suggestPort())

		gossipPeers = append(gossipPeers, peer.Peer{Addr: "localhost", Port: mockPort})
		mockPeers = append(mockPeers, peer.Peer{Addr: "localhost", Port: gossipPort})

		gossipMsgBuf = buffer.NewMessageBuffer()
		gossipMsgBuf.AddMessage(buffer.Message{
			ID:          fmt.Sprintf("%d", rand.Int31()),
			Msg:         fmt.Sprintf("%d", rand.Int31()),
			GossipCount: rand.Int(),
		})
		mockMsgBuf = buffer.NewMessageBuffer()

		gossipCfg = config.GossipConfig{
			Addr:    "localhost",
			Port:    gossipPort,
			PeerBuf: gossipPeers,
			MsgBuf:  &gossipMsgBuf,
		}
		httpCfg = config.HTTPConfig{
			Addr:    "localhost",
			Port:    gossipPort,
			PeerBuf: gossipPeers,
			MsgBuf:  &gossipMsgBuf,
		}
		mockCfg = config.HTTPConfig{
			Addr:    "localhost",
			Port:    mockPort,
			PeerBuf: mockPeers,
			MsgBuf:  &mockMsgBuf,
		}

		gossipStop = make(chan struct{})
		httpStop = make(chan struct{})
		mockStop = make(chan struct{})

		gossip = New(gossipCfg)
	})

	AfterEach(func() {
		close(gossipStop)
		close(mockStop)
	})

	It("first unit test", func() {
		go func() {
			mockHTTPServer := server.New(mockCfg)
			err := mockHTTPServer.Start(mockStop)
			Expect(err).To(Succeed())
		}()

		go func() {
			gossipHTTPServer := server.New(httpCfg)
			err := gossipHTTPServer.Start(httpStop)
			Expect(err).To(Succeed())
		}()

		// wait for starting http servers
		time.Sleep(time.Second)

		go func() {
			gossip.Start(gossipStop)
		}()

		Eventually(func() bool {
			return gossipMsgBuf.SameMessages(&mockMsgBuf)
		}, timeout).Should(Equal(true))
	})
})