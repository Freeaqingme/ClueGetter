package postfixPolicy

import (
	"fmt"
	"net"
	"log"
	"bufio"
	"time"
)

const (
	CONN_HOST = "localhost"
	CONN_PORT = "10032"
	CONN_TYPE = "tcp"
)

func Start(c chan int) {
	l, err := net.Listen("tcp", CONN_HOST + ":" + CONN_PORT)
	if nil != err {
		log.Fatalln(err)
	}

	log.Println("Now listening on " + CONN_HOST + ":" + CONN_PORT)
	go waitForConnections(l)

	<-c
	l.Close()
	c <- 1
}

func waitForConnections(l net.Listener) {
	backoffTime := 0

	for {
		conn, err := l.Accept()
		if err != nil {
			if (backoffTime < 8000) {
				backoffTime = int(float32(backoffTime) * float32(1.02)) + 250
				log.Println(fmt.Sprintf("Could not accept connection: %s. Backing off for %d"),
					err, backoffTime);
				time.Sleep(time.Duration(backoffTime) * time.Millisecond)
			} else {
				log.Fatalln(fmt.Sprintf("Could not accept new connection. Backing out: %d", err))
			}
		}

		backoffTime = int(float32(backoffTime) / float32(1.1))
		go handleRequest(conn)
	}
}

func handleRequest(conn net.Conn) {
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		s := scanner.Text()
		fmt.Println(s)

		if s == "" {
			break
		}
	}

	if err := scanner.Err(); err != nil {
		log.Println(err)
	}

	conn.Write([]byte("Good bye\n"))
	conn.Close()
}
