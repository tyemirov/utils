package jseval

import (
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

func TestNewSOCKSForwarderAndClose(t *testing.T) {
	// Use a valid SOCKS5 URL (the upstream won't be connected to in this test)
	forwarder, err := newSOCKSForwarder("socks5://user:pass@127.0.0.1:19999")
	if err != nil {
		t.Fatalf("newSOCKSForwarder: %v", err)
	}
	if forwarder.addr == "" {
		t.Fatal("expected non-empty listener address")
	}
	forwarder.close()
}

func TestNewSOCKSForwarderWithoutAuth(t *testing.T) {
	forwarder, err := newSOCKSForwarder("socks5://127.0.0.1:19999")
	if err != nil {
		t.Fatalf("newSOCKSForwarder without auth: %v", err)
	}
	forwarder.close()
}

func TestNewSOCKSForwarderInvalidURL(t *testing.T) {
	_, err := newSOCKSForwarder("://invalid\x00url")
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

// mockDialer is a proxy.Dialer that connects to a local echo server.
type mockDialer struct {
	targetListener net.Listener
}

func (d *mockDialer) Dial(network, addr string) (net.Conn, error) {
	return net.Dial("tcp", d.targetListener.Addr().String())
}

// failDialer always fails to dial.
type failDialer struct{}

func (d *failDialer) Dial(network, addr string) (net.Conn, error) {
	return nil, &net.OpError{Op: "dial", Net: "tcp", Err: io.EOF}
}

// startEchoServer starts a TCP server that echoes back everything it receives.
func startEchoServer(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("echo server listen: %v", err)
	}
	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go func() {
				defer conn.Close()
				io.Copy(conn, conn)
			}()
		}
	}()
	return listener
}

func TestHandleConnectionDomainNameConnect(t *testing.T) {
	echoServer := startEchoServer(t)
	defer echoServer.Close()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	fwd := &socksForwarder{
		listener: listener,
		addr:     listener.Addr().String(),
		dialer:   &mockDialer{targetListener: echoServer},
	}
	go fwd.acceptLoop()
	defer fwd.close()

	conn, err := net.DialTimeout("tcp", fwd.addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial forwarder: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	// SOCKS5 handshake: version 5, 1 auth method (no auth)
	conn.Write([]byte{0x05, 0x01, 0x00})

	// Read server choice
	authReply := make([]byte, 2)
	io.ReadFull(conn, authReply)
	if authReply[0] != 0x05 || authReply[1] != 0x00 {
		t.Fatalf("unexpected auth reply: %v", authReply)
	}

	// CONNECT to example.com:80 (domain name type 0x03)
	domain := "example.com"
	request := []byte{0x05, 0x01, 0x00, 0x03, byte(len(domain))}
	request = append(request, []byte(domain)...)
	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, 80)
	request = append(request, portBytes...)
	conn.Write(request)

	// Read connect reply
	connectReply := make([]byte, 10)
	io.ReadFull(conn, connectReply)
	if connectReply[1] != 0x00 {
		t.Fatalf("expected success reply, got status %d", connectReply[1])
	}

	// Send data through the tunnel — echo server should return it
	testData := []byte("hello through socks5")
	conn.Write(testData)

	buf := make([]byte, len(testData))
	io.ReadFull(conn, buf)
	if string(buf) != string(testData) {
		t.Errorf("expected %q, got %q", testData, buf)
	}
}

func TestHandleConnectionIPv4Connect(t *testing.T) {
	echoServer := startEchoServer(t)
	defer echoServer.Close()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	fwd := &socksForwarder{
		listener: listener,
		addr:     listener.Addr().String(),
		dialer:   &mockDialer{targetListener: echoServer},
	}
	go fwd.acceptLoop()
	defer fwd.close()

	conn, err := net.DialTimeout("tcp", fwd.addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial forwarder: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	// SOCKS5 handshake
	conn.Write([]byte{0x05, 0x01, 0x00})
	authReply := make([]byte, 2)
	io.ReadFull(conn, authReply)

	// CONNECT to 127.0.0.1:80 (IPv4 type 0x01)
	request := []byte{0x05, 0x01, 0x00, 0x01, 127, 0, 0, 1}
	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, 80)
	request = append(request, portBytes...)
	conn.Write(request)

	connectReply := make([]byte, 10)
	io.ReadFull(conn, connectReply)
	if connectReply[1] != 0x00 {
		t.Fatalf("expected success reply, got status %d", connectReply[1])
	}

	testData := []byte("ipv4 tunnel data")
	conn.Write(testData)
	buf := make([]byte, len(testData))
	io.ReadFull(conn, buf)
	if string(buf) != string(testData) {
		t.Errorf("expected %q, got %q", testData, buf)
	}
}

func TestHandleConnectionIPv6Connect(t *testing.T) {
	echoServer := startEchoServer(t)
	defer echoServer.Close()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	fwd := &socksForwarder{
		listener: listener,
		addr:     listener.Addr().String(),
		dialer:   &mockDialer{targetListener: echoServer},
	}
	go fwd.acceptLoop()
	defer fwd.close()

	conn, err := net.DialTimeout("tcp", fwd.addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial forwarder: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	// SOCKS5 handshake
	conn.Write([]byte{0x05, 0x01, 0x00})
	authReply := make([]byte, 2)
	io.ReadFull(conn, authReply)

	// CONNECT to [::1]:80 (IPv6 type 0x04)
	request := []byte{0x05, 0x01, 0x00, 0x04}
	ipv6 := net.ParseIP("::1").To16()
	request = append(request, ipv6...)
	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, 80)
	request = append(request, portBytes...)
	conn.Write(request)

	connectReply := make([]byte, 10)
	io.ReadFull(conn, connectReply)
	if connectReply[1] != 0x00 {
		t.Fatalf("expected success reply, got status %d", connectReply[1])
	}
}

func TestHandleConnectionDialFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	fwd := &socksForwarder{
		listener: listener,
		addr:     listener.Addr().String(),
		dialer:   &failDialer{},
	}
	go fwd.acceptLoop()
	defer fwd.close()

	conn, err := net.DialTimeout("tcp", fwd.addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial forwarder: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	// SOCKS5 handshake
	conn.Write([]byte{0x05, 0x01, 0x00})
	authReply := make([]byte, 2)
	io.ReadFull(conn, authReply)

	// CONNECT request (domain)
	domain := "unreachable.example.com"
	request := []byte{0x05, 0x01, 0x00, 0x03, byte(len(domain))}
	request = append(request, []byte(domain)...)
	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, 80)
	request = append(request, portBytes...)
	conn.Write(request)

	// Read failure reply
	connectReply := make([]byte, 10)
	io.ReadFull(conn, connectReply)
	if connectReply[1] != 0x01 {
		t.Fatalf("expected failure reply (0x01), got status %d", connectReply[1])
	}
}

func TestHandleConnectionInvalidSOCKSVersion(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	fwd := &socksForwarder{
		listener: listener,
		addr:     listener.Addr().String(),
		dialer:   &failDialer{},
	}
	go fwd.acceptLoop()
	defer fwd.close()

	conn, err := net.DialTimeout("tcp", fwd.addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial forwarder: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))

	// Send SOCKS4 version — should be rejected
	conn.Write([]byte{0x04, 0x01, 0x00})

	// Connection should be closed by the handler
	buf := make([]byte, 1)
	_, readErr := conn.Read(buf)
	if readErr == nil {
		t.Fatal("expected connection to be closed after invalid SOCKS version")
	}
}

func TestHandleConnectionInvalidCommand(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	fwd := &socksForwarder{
		listener: listener,
		addr:     listener.Addr().String(),
		dialer:   &failDialer{},
	}
	go fwd.acceptLoop()
	defer fwd.close()

	conn, err := net.DialTimeout("tcp", fwd.addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial forwarder: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))

	// Valid SOCKS5 handshake
	conn.Write([]byte{0x05, 0x01, 0x00})
	authReply := make([]byte, 2)
	io.ReadFull(conn, authReply)

	// Send BIND command (0x02) instead of CONNECT (0x01)
	conn.Write([]byte{0x05, 0x02, 0x00, 0x01})

	// Connection should be closed
	buf := make([]byte, 1)
	_, readErr := conn.Read(buf)
	if readErr == nil {
		t.Fatal("expected connection to be closed after invalid command")
	}
}

func TestHandleConnectionUnknownAddressType(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	fwd := &socksForwarder{
		listener: listener,
		addr:     listener.Addr().String(),
		dialer:   &failDialer{},
	}
	go fwd.acceptLoop()
	defer fwd.close()

	conn, err := net.DialTimeout("tcp", fwd.addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial forwarder: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))

	// Valid SOCKS5 handshake
	conn.Write([]byte{0x05, 0x01, 0x00})
	authReply := make([]byte, 2)
	io.ReadFull(conn, authReply)

	// CONNECT with unknown address type 0xFF
	conn.Write([]byte{0x05, 0x01, 0x00, 0xFF})

	// Connection should be closed
	buf := make([]byte, 1)
	_, readErr := conn.Read(buf)
	if readErr == nil {
		t.Fatal("expected connection to be closed after unknown address type")
	}
}

func TestHandleConnectionReadHeaderError(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	fwd := &socksForwarder{
		listener: listener,
		addr:     listener.Addr().String(),
		dialer:   &failDialer{},
	}
	go fwd.acceptLoop()
	defer fwd.close()

	conn, err := net.DialTimeout("tcp", fwd.addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial forwarder: %v", err)
	}

	// Close immediately — handler should get a read error
	conn.Close()

	// Give handler time to process
	time.Sleep(50 * time.Millisecond)
}
