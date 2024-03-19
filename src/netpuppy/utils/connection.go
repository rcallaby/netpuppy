/*
	This code is intentionally redundant!
	In order to implement unit testing on this code
	we want to avoid creating REAL socket connections
	while running tests.

	There are two shared interfaces (Socket & ConnectionGetter)
	which allow	us to isolate the actual creation of a socket
	& still write tests for this code. RealSocket & TestSocket
	are checked against the Socket interface. RealConnectionGetter
	& TestConnectionGetter are checked against the ConnectionGetter
	interface.

	TEST code:
	...includes the TestSocket & TestConnectionGetter structs
	which implement a fake socket using fake connection methods.
	The	methods will be called for unit testing and they return the
	same value-types as the REAL code.

	REAL code:
	The only code which can be used to interact w/ the REAL socket
	connection is RealSocket struct. Both the Real and Test sockets
	are verified by the Socket interface.

	The REAL socket is created and attached to the RealSocket struct
	in the GetConnectionFromClient GetConnectionFromListener methods
	attached to the RealConnectionGetter struct (being checked against
	ConnectionGetterInterface).

	Once created and attached to RealSocket, the socket can be read from/ written
	to and closed by using the Read() Write() & Close() methods. The real socket
	(net.Conn) object shouldn't be handled anywhere else (or testing will break).

	TESTING:
	If something changes about the code in a way that deviates from the
	blueprinting done by the interfaces, tests will fail.
*/

package utils

import (
	"fmt"
	"log"
	"net"
	"os"
	"time"
)

// SHARED Code:
// ..... The Socket and ConnectionGetter interfaces are used by both real & test code:
type Socket interface {
	// Used to check real (RealSocket) & test (TestSocket) structs
	Read() ([]byte, error)
	Write([]byte) (int, error)
	Close() error
	SetSocketReadDeadline(int) error
}

type ConnectionGetter interface {
	// Used to check the real (RealConnectionGetter) & test (TestConnectionGetter) structs:
	GetConnectionFromListener(int, string) Socket
	GetConnectionFromClient(int, string) Socket
}

// TEST Code:
type TestSocket struct {
	Port    int
	Address string
}

type TestConnectionGetter struct {
	// Leave empty
}

func (c TestConnectionGetter) GetConnectionFromClient(rPort int, address string) Socket {
	testClientConnection := TestSocket{Port: rPort, Address: address}
	return testClientConnection
}

func (c TestConnectionGetter) GetConnectionFromListener(rPort int, address string) Socket {
	testListenerConnection := TestSocket{Port: rPort, Address: address}
	return testListenerConnection
}

func (i TestSocket) Read() ([]byte, error) {
	testByteArr := []byte("tiddies")
	var testErr error

	return testByteArr, testErr
}

func (i TestSocket) Write([]byte) (int, error) {
	var testInt int = 22
	var testErrorNil error = nil
	return testInt, testErrorNil
}

func (i TestSocket) Close() error {
	var testCloseErr error
	return testCloseErr
}

func (i TestSocket) SetSocketReadDeadline(miliseconds int) error {
	var testDeadlineErr error
	return testDeadlineErr
}

// REAL code:
type RealSocket struct { // This is the only code which holds the ACTUAL net connection:
	RrealSocket net.Conn
}

type RealConnectionGetter struct {
	// Leave empty
}

// Read from the ACTUAL socket on RealSocket struct:
func (s RealSocket) Read() ([]byte, error) {
	var buffer []byte = make([]byte, 1024)
	var numberOfBytesSent int = 0
	var err error

	// 'numberOfBytes' will tell us how many bytes were read from socket, use to index into buffer:
	numberOfBytesSent, err = s.RrealSocket.Read(buffer)
	return buffer[:numberOfBytesSent], err
}

// Write to the ACTUAL socket on RealSocket struct:
func (s RealSocket) Write(userInput []byte) (int, error) {
	var writeSuccess int
	var err error

	writeSuccess, err = s.RrealSocket.Write(userInput)
	return writeSuccess, err
}

// Close the ACTUAL socket:
func (s RealSocket) Close() error {
	var err error = s.RrealSocket.Close()
	return err
}

// Set read deadline on ACTUAL socket:
func (s RealSocket) SetSocketReadDeadline(miliseconds int) error {
	timeout := time.Duration(miliseconds) * time.Millisecond
	err := s.RrealSocket.SetReadDeadline(time.Now().Add(timeout))

	return err
}

// These next 2 function create the ACTUAL socket and attach the connection to RealSocket
// ..... Create client-type socket & attach to RealSocket:
func (r RealConnectionGetter) GetConnectionFromClient(rPort int, address string) Socket {
	var clientConnection net.Conn
	var err error
	var remoteHost string = fmt.Sprintf("%v:%v", address, rPort)
	var pointerToRealSocket *RealSocket

	// Get client connection:
	clientConnection, err = net.Dial("tcp", remoteHost)
	if err != nil {
		log.Fatalf("Error creating client connection (connection.go): %v\n", err)
	}

	// Attach connection to RealSocket & get the pointer to the instance:
	pointerToRealSocket = &RealSocket{RrealSocket: clientConnection}

	fmt.Printf("Address to socket in connection.go = %p\n", pointerToRealSocket)
	return pointerToRealSocket
}

// Creat listener-type socket & attach to RealSocket:
func (r RealConnectionGetter) GetConnectionFromListener(rPort int, address string) Socket {
	var listenerConnection net.Conn
	var err error
	var localPort string = fmt.Sprintf(":%v", rPort)
	var pointerToRealSocket *RealSocket

	// Listener created first:
	listener, err1 := net.Listen("tcp", localPort)
	if err1 != nil {
		fmt.Printf("Error when creating listener connection: %v\n", err1)
		os.Stderr.WriteString(" " + err1.Error() + "\n")
		os.Exit(1)
	}

	// This ensures the listener closes before the function returns:
	defer listener.Close()

	// Create the connection using listener.Accept():
	listenerConnection, err = listener.Accept()
	if err != nil {
		fmt.Printf("Error when creating listener connection: %v\n", err)
		os.Stderr.WriteString(" " + err.Error() + "\n")
		os.Exit(1)
	}

	// Attach the connection to the RealSocket struct & return the pointer to the instance:
	pointerToRealSocket = &RealSocket{RrealSocket: listenerConnection}

	fmt.Printf("socket address in connection.go: %p\n", pointerToRealSocket)
	return pointerToRealSocket
}
