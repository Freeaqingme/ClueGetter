/**
* See also: http://www.postfix.org/SMTPD_POLICY_README.html
**/

package postfix

import (
	"cluegetter/module"
	"bufio"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"time"
)

const (
	CONN_HOST = "localhost"
	CONN_PORT = "10032"
	CONN_TYPE = "tcp"
)

func PolicyStart(c chan int) {
	module.Foo()

	l, err := net.Listen("tcp", CONN_HOST+":"+CONN_PORT)
	if nil != err {
		log.Fatalln(err)
	}

	log.Println("Now listening on " + CONN_HOST + ":" + CONN_PORT)
	go policyWaitForConnections(l)

	<-c
	l.Close()
	c <- 1
}

func policyWaitForConnections(l net.Listener) {
	backoffTime := float32(0)

	for {
		conn, err := l.Accept()
		if err != nil && backoffTime < 8000 {
			backoffTime = (backoffTime * 1.02) + 250
			log.Println(fmt.Sprintf("Could not accept connection: %s. Backing off for %d",
				err, backoffTime))
			time.Sleep(time.Duration(backoffTime) * time.Millisecond)
			break
		} else if err != nil {
			log.Fatalln(fmt.Sprintf("Could not accept new connection. Backing out: %d", err))
		}

		backoffTime = backoffTime / 1.1
		go policyHandleRequest(conn)
	}
}

func policyHandleRequest(conn net.Conn) {
	defer conn.Close()
	message := ""

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		s := scanner.Text()
		if s == "" {
			break
		}
		message = message + "\n" + s
	}

	if err := scanner.Err(); err != nil {
		log.Println(err)
		return
	}

	response := policyGetResponseForMessage(strings.TrimSpace(message))

	conn.Write([]byte(response + "\n"))
	fmt.Println(response)
}

func policyGetResponseForMessage(message string) string {
	policyRequest, err := policyParseMessage(message)
	if err != nil {
		log.Println(err)
		return "dunno"
	}

	fmt.Println(policyRequest)
	return ""
}

func policyParseMessage(message string) (map[string]string, error) {
	policyRequest := make(map[string]string)

	lines := strings.Split(message, "\n")
	for _, line := range lines {
		keyVal := strings.SplitN(line, "=", 2)
		if len(keyVal) != 2 {
			return nil, errors.New(fmt.Sprintf("Could not parse line. Line was: '%s'", line))
		}

		policyRequest[keyVal[0]] = keyVal[1]
	}

	return policyRequest, nil
}
