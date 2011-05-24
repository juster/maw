/*	ftp.go
	Simple FTP file fetching.
*/

package main

import (
	"io"
	"os"
	"net"
	proto "net/textproto"
	"fmt"
	"strings"
	"strconv"
	"regexp"
)

const (
	ThreeDigits = "([0-9]+)"
	AddrRegexp = ThreeDigits + "," + ThreeDigits + "," + ThreeDigits + "," + ThreeDigits + "," +
		ThreeDigits + "," + ThreeDigits
	AnonPasswd = "maw@juster.us"
	PreLogin = iota
	LoggedIn = iota
	PostLogout = iota
)

var (
	pasvMatch = regexp.MustCompile("^Entering Passive Mode \\("+AddrRegexp+"\\)\\.")
)

type FtpConn struct {
	state, pendingTransfers int
	p *proto.Conn
}

func DialFtp(addr string) (*FtpConn, os.Error) {
	conn, err := proto.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	return &FtpConn{PreLogin, 0, conn}, nil
}

func (ftp *FtpConn) expectResp(expectedCode int) (string, os.Error) {
	// Loop is here in case we get a 226 response and need to read again.
	for {
		code, msg, err := ftp.p.ReadResponse(expectedCode)
	
		// This code is sent whenever the data connection is closed (i.e. file is finished downloading)
		if code == 226 {
			if ftp.pendingTransfers > 0 {
				// Loop and read the response again
				ftp.pendingTransfers--
				continue
			} else {
				// We weren't expecting any 226, return an error
				return msg, &proto.Error{code, msg}
			}
		}

		return msg, err
	}

	return "", os.NewError("Internal error in ftp.go:expectResp()")
}

func (ftp *FtpConn) anonLogin() os.Error {
	if _, err := ftp.expectResp(220); err != nil {
		return err
	}

	// Login takes a USER command then a PASS command.
	if err := ftp.p.PrintfLine("USER Anonymous"); err != nil {
		return err
	}
	_, err := ftp.expectResp(331)
	if err != nil {
		return loginError(err)
	}
	
	if err = ftp.p.PrintfLine("PASS %s", AnonPasswd); err != nil {
		return err
	}
	_, err = ftp.expectResp(230)
	if err != nil {
		return loginError(err)
	}
	
	ftp.state = LoggedIn
	return nil
}

func (ftp *FtpConn) Fetch(rpath string) (io.Reader, os.Error) {
	var err os.Error
	switch ftp.state {
	case PostLogout: // wtf?
		ftp.p.PrintfLine("REIN")
		fallthrough
	case PreLogin:
		err = ftp.anonLogin()
	case LoggedIn:
		// awesome, nothing to do
	}

	if err != nil {
		return nil, err
	}

	// The default data transmission type is ASCII, be sure to set it to Image (binary)
	if err := ftp.p.PrintfLine("TYPE I"); err != nil {
		return nil, err
	}
	_, err = ftp.expectResp(200)
	if err != nil {
		return nil, dlError(err)
	}
	
	// Make sure we use passive mode for downloading. With passive mode the client
	// starts the data connection to the server, not the other way around.
	if err := ftp.p.PrintfLine("PASV"); err != nil {
		return nil, err
	}
	msg, err := ftp.expectResp(227)
	if err != nil {
		return nil, dlError(err)
	}
	
	var addrstr string
	if addrcomps := pasvMatch.FindStringSubmatch(msg); len(addrcomps) == 7 {
		addrstr = addrToStr(addrcomps[1:])
		if addrstr != "" {
			goto PasvSuccess
		}
	}
	return nil, os.NewError("Failed to download, server failed to enter passive mode")

PasvSuccess:
	// Get our data connection ready.
	dconn, err := net.Dial("tcp", addrstr)
	if err != nil {
		return nil, dlError(err)
	}
	
	// Now we can signal the server to start the file transfer.
	if err := ftp.p.PrintfLine("RETR %s", rpath); err != nil {
		dconn.Close()
		return nil, err
	}
	_, err = ftp.expectResp(150)
	if err != nil {
		dconn.Close()
		return nil, dlError(err)
	}

	return dconn, nil
}

func loginError(err os.Error) os.Error {
	return ftpError("ftp login failed", err)
}

func dlError(err os.Error) os.Error {
	return ftpError("ftp download failed", err)
}

func ftpError(prefix string, err os.Error) os.Error {
	msg := fmt.Sprintf("%s:%s", prefix, err.String())
	return os.NewError(msg)
}

// The address and port specified for the data connection is given as text, with each
// IP byte separated by commas, followed by two bytes for the port number.
func addrToStr(addr []string) string {
	if len(addr) != 6 {
		return ""
	}
	
	// The host is pretty easy to convert into an address usable by Dial...
	host := strings.Join(addr[0:4], ".")
	
	// The port is given as two numbers, a high byte and a low byte.
	port, err := strconv.Atoi(addr[4])
	if err != nil {
		return ""
	}
	port <<= 8
	lobyte, err := strconv.Atoi(addr[5])
	if err != nil {
		return ""
	}
	port |= lobyte
	return fmt.Sprintf("%s:%d", host, port)
}

func (ftp *FtpConn) Quit() os.Error {
	err := ftp.p.PrintfLine("QUIT")
	if err != nil {
		return err
	}
	_, err = ftp.expectResp(221)
	if err != nil {
		return err
	}

	ftp.state = PostLogout
	return nil
}

func (ftp *FtpConn) Close() os.Error {
	if ftp.state == LoggedIn {
		if err := ftp.Quit(); err != nil {
			return err
		}
	}
	return ftp.p.Close()
}