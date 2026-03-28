package browsertransport

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

func TestNewSOCKSForwarderAndClose(t *testing.T) {
	forwarder, forwarderError := newSOCKSForwarder("socks5://user:pass@127.0.0.1:19999")
	if forwarderError != nil {
		t.Fatalf("newSOCKSForwarder() error = %v", forwarderError)
	}
	if forwarder.addr == "" {
		t.Fatal("expected non-empty listener address")
	}
	forwarder.close()
}

func TestNewSOCKSForwarderWithoutAuth(t *testing.T) {
	forwarder, forwarderError := newSOCKSForwarder("socks5://127.0.0.1:19999")
	if forwarderError != nil {
		t.Fatalf("newSOCKSForwarder() error = %v", forwarderError)
	}
	forwarder.close()
}

func TestNewSOCKSForwarderInvalidURL(t *testing.T) {
	if _, forwarderError := newSOCKSForwarder("http://proxy.example.com:\x00"); forwarderError == nil {
		t.Fatal("expected error for invalid URL")
	}
}

func TestNewSOCKSForwarderListenError(t *testing.T) {
	restoreHooks := resetBrowserTransportHooks()
	defer restoreHooks()

	netListen = func(network string, address string) (net.Listener, error) {
		return nil, errors.New("listen failed")
	}

	if _, forwarderError := newSOCKSForwarder("socks5://user:pass@127.0.0.1:19999"); forwarderError == nil {
		t.Fatal("expected listen error")
	}
}

type mockDialer struct {
	targetListener net.Listener
}

func (dialer *mockDialer) Dial(network string, address string) (net.Conn, error) {
	return net.Dial("tcp", dialer.targetListener.Addr().String())
}

type captureDialer struct {
	targetListener net.Listener
	mu             sync.Mutex
	address        string
}

func (dialer *captureDialer) Dial(network string, address string) (net.Conn, error) {
	dialer.mu.Lock()
	dialer.address = address
	dialer.mu.Unlock()
	return net.Dial("tcp", dialer.targetListener.Addr().String())
}

func (dialer *captureDialer) Address() string {
	dialer.mu.Lock()
	defer dialer.mu.Unlock()
	return dialer.address
}

type failDialer struct{}

func (dialer *failDialer) Dial(network string, address string) (net.Conn, error) {
	return nil, &net.OpError{Op: "dial", Net: "tcp", Err: io.EOF}
}

func startEchoServer(t *testing.T) net.Listener {
	t.Helper()

	listener, listenError := net.Listen("tcp", "127.0.0.1:0")
	if listenError != nil {
		t.Fatalf("net.Listen() error = %v", listenError)
	}

	go func() {
		for {
			connection, acceptError := listener.Accept()
			if acceptError != nil {
				return
			}
			go func() {
				defer connection.Close()
				_, _ = io.Copy(connection, connection)
			}()
		}
	}()

	return listener
}

func TestHandleConnectionDomainNameConnect(t *testing.T) {
	echoServer := startEchoServer(t)
	defer echoServer.Close()

	listener, listenError := net.Listen("tcp", "127.0.0.1:0")
	if listenError != nil {
		t.Fatalf("net.Listen() error = %v", listenError)
	}

	forwarder := &socksForwarder{
		listener: listener,
		addr:     listener.Addr().String(),
		dialer:   &mockDialer{targetListener: echoServer},
	}
	go forwarder.acceptLoop()
	defer forwarder.close()

	connection, dialError := net.DialTimeout("tcp", forwarder.addr, 2*time.Second)
	if dialError != nil {
		t.Fatalf("DialTimeout() error = %v", dialError)
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(5 * time.Second))

	_, _ = connection.Write([]byte{0x05, 0x01, 0x00})

	authReply := make([]byte, 2)
	_, _ = io.ReadFull(connection, authReply)
	if authReply[0] != 0x05 || authReply[1] != 0x00 {
		t.Fatalf("unexpected auth reply: %v", authReply)
	}

	domain := "example.com"
	request := []byte{0x05, 0x01, 0x00, 0x03, byte(len(domain))}
	request = append(request, []byte(domain)...)
	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, 80)
	request = append(request, portBytes...)
	_, _ = connection.Write(request)

	connectReply := make([]byte, 10)
	_, _ = io.ReadFull(connection, connectReply)
	if connectReply[1] != 0x00 {
		t.Fatalf("expected success reply, got status %d", connectReply[1])
	}

	testData := []byte("hello through socks5")
	_, _ = connection.Write(testData)
	buffer := make([]byte, len(testData))
	_, _ = io.ReadFull(connection, buffer)
	if string(buffer) != string(testData) {
		t.Fatalf("buffer = %q, want %q", buffer, testData)
	}
}

func TestHandleConnectionIPv4Connect(t *testing.T) {
	echoServer := startEchoServer(t)
	defer echoServer.Close()

	listener, listenError := net.Listen("tcp", "127.0.0.1:0")
	if listenError != nil {
		t.Fatalf("net.Listen() error = %v", listenError)
	}

	forwarder := &socksForwarder{
		listener: listener,
		addr:     listener.Addr().String(),
		dialer:   &mockDialer{targetListener: echoServer},
	}
	go forwarder.acceptLoop()
	defer forwarder.close()

	connection, dialError := net.DialTimeout("tcp", forwarder.addr, 2*time.Second)
	if dialError != nil {
		t.Fatalf("DialTimeout() error = %v", dialError)
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(5 * time.Second))

	_, _ = connection.Write([]byte{0x05, 0x01, 0x00})
	authReply := make([]byte, 2)
	_, _ = io.ReadFull(connection, authReply)

	request := []byte{0x05, 0x01, 0x00, 0x01, 127, 0, 0, 1}
	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, 80)
	request = append(request, portBytes...)
	_, _ = connection.Write(request)

	connectReply := make([]byte, 10)
	_, _ = io.ReadFull(connection, connectReply)
	if connectReply[1] != 0x00 {
		t.Fatalf("expected success reply, got status %d", connectReply[1])
	}

	testData := []byte("ipv4 tunnel data")
	_, _ = connection.Write(testData)
	buffer := make([]byte, len(testData))
	_, _ = io.ReadFull(connection, buffer)
	if string(buffer) != string(testData) {
		t.Fatalf("buffer = %q, want %q", buffer, testData)
	}
}

func TestHandleConnectionIPv6Connect(t *testing.T) {
	echoServer := startEchoServer(t)
	defer echoServer.Close()

	listener, listenError := net.Listen("tcp", "127.0.0.1:0")
	if listenError != nil {
		t.Fatalf("net.Listen() error = %v", listenError)
	}

	dialer := &captureDialer{targetListener: echoServer}
	forwarder := &socksForwarder{
		listener: listener,
		addr:     listener.Addr().String(),
		dialer:   dialer,
	}
	go forwarder.acceptLoop()
	defer forwarder.close()

	connection, dialError := net.DialTimeout("tcp", forwarder.addr, 2*time.Second)
	if dialError != nil {
		t.Fatalf("DialTimeout() error = %v", dialError)
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(5 * time.Second))

	_, _ = connection.Write([]byte{0x05, 0x01, 0x00})
	authReply := make([]byte, 2)
	_, _ = io.ReadFull(connection, authReply)

	request := []byte{0x05, 0x01, 0x00, 0x04}
	request = append(request, net.ParseIP("::1").To16()...)
	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, 80)
	request = append(request, portBytes...)
	_, _ = connection.Write(request)

	connectReply := make([]byte, 10)
	_, _ = io.ReadFull(connection, connectReply)
	if connectReply[1] != 0x00 {
		t.Fatalf("expected success reply, got status %d", connectReply[1])
	}

	testData := []byte("ipv6 tunnel data")
	_, _ = connection.Write(testData)
	buffer := make([]byte, len(testData))
	_, _ = io.ReadFull(connection, buffer)
	if string(buffer) != string(testData) {
		t.Fatalf("buffer = %q, want %q", buffer, testData)
	}
	if dialer.Address() != "[::1]:80" {
		t.Fatalf("dial address = %q, want %q", dialer.Address(), "[::1]:80")
	}
}

func TestHandleConnectionUnsupportedCommandAndDialFailure(t *testing.T) {
	listener, listenError := net.Listen("tcp", "127.0.0.1:0")
	if listenError != nil {
		t.Fatalf("net.Listen() error = %v", listenError)
	}

	forwarder := &socksForwarder{
		listener: listener,
		addr:     listener.Addr().String(),
		dialer:   &failDialer{},
	}
	go forwarder.acceptLoop()
	defer forwarder.close()

	connection, dialError := net.DialTimeout("tcp", forwarder.addr, 2*time.Second)
	if dialError != nil {
		t.Fatalf("DialTimeout() error = %v", dialError)
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(5 * time.Second))

	_, _ = connection.Write([]byte{0x05, 0x01, 0x00})
	authReply := make([]byte, 2)
	_, _ = io.ReadFull(connection, authReply)

	_, _ = connection.Write([]byte{0x05, 0x02, 0x00, 0x01, 127, 0, 0, 1, 0, 80})
	time.Sleep(50 * time.Millisecond)

	secondConnection, secondDialError := net.DialTimeout("tcp", forwarder.addr, 2*time.Second)
	if secondDialError != nil {
		t.Fatalf("DialTimeout() error = %v", secondDialError)
	}
	defer secondConnection.Close()
	_ = secondConnection.SetDeadline(time.Now().Add(5 * time.Second))

	_, _ = secondConnection.Write([]byte{0x05, 0x01, 0x00})
	_, _ = io.ReadFull(secondConnection, authReply)
	_, _ = secondConnection.Write([]byte{0x05, 0x01, 0x00, 0x01, 127, 0, 0, 1, 0, 80})

	connectReply := make([]byte, 10)
	_, _ = io.ReadFull(secondConnection, connectReply)
	if connectReply[1] != 0x01 {
		t.Fatalf("expected failure reply, got status %d", connectReply[1])
	}
}

func TestHandleConnectionReadErrorBranches(t *testing.T) {
	listener, listenError := net.Listen("tcp", "127.0.0.1:0")
	if listenError != nil {
		t.Fatalf("net.Listen() error = %v", listenError)
	}

	forwarder := &socksForwarder{
		listener: listener,
		addr:     listener.Addr().String(),
		dialer:   &failDialer{},
	}
	go forwarder.acceptLoop()
	defer forwarder.close()

	connection, dialError := net.DialTimeout("tcp", forwarder.addr, 2*time.Second)
	if dialError != nil {
		t.Fatalf("DialTimeout() error = %v", dialError)
	}
	_, _ = connection.Write([]byte{0x05, 0x01})
	_ = connection.Close()

	secondConnection, secondDialError := net.DialTimeout("tcp", forwarder.addr, 2*time.Second)
	if secondDialError != nil {
		t.Fatalf("DialTimeout() error = %v", secondDialError)
	}
	_, _ = secondConnection.Write([]byte{0x05, 0x01, 0x00})
	authReply := make([]byte, 2)
	_, _ = io.ReadFull(secondConnection, authReply)
	_ = secondConnection.Close()

	time.Sleep(50 * time.Millisecond)
}

func TestHandleConnectionInvalidHeaderAndUnsupportedAddressType(t *testing.T) {
	listener, listenError := net.Listen("tcp", "127.0.0.1:0")
	if listenError != nil {
		t.Fatalf("net.Listen() error = %v", listenError)
	}

	forwarder := &socksForwarder{
		listener: listener,
		addr:     listener.Addr().String(),
		dialer:   &failDialer{},
	}
	go forwarder.acceptLoop()
	defer forwarder.close()

	connection, dialError := net.DialTimeout("tcp", forwarder.addr, 2*time.Second)
	if dialError != nil {
		t.Fatalf("DialTimeout() error = %v", dialError)
	}
	_ = connection.Close()

	secondConnection, secondDialError := net.DialTimeout("tcp", forwarder.addr, 2*time.Second)
	if secondDialError != nil {
		t.Fatalf("DialTimeout() error = %v", secondDialError)
	}
	defer secondConnection.Close()
	_ = secondConnection.SetDeadline(time.Now().Add(5 * time.Second))
	_, _ = secondConnection.Write([]byte{0x04, 0x01, 0x00})
	time.Sleep(50 * time.Millisecond)

	thirdConnection, thirdDialError := net.DialTimeout("tcp", forwarder.addr, 2*time.Second)
	if thirdDialError != nil {
		t.Fatalf("DialTimeout() error = %v", thirdDialError)
	}
	defer thirdConnection.Close()
	_ = thirdConnection.SetDeadline(time.Now().Add(5 * time.Second))
	_, _ = thirdConnection.Write([]byte{0x05, 0x01, 0x00})
	authReply := make([]byte, 2)
	_, _ = io.ReadFull(thirdConnection, authReply)
	_, _ = thirdConnection.Write([]byte{0x05, 0x01, 0x00, 0x05})
	time.Sleep(50 * time.Millisecond)
}
