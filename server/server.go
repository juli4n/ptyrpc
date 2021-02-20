package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"os/exec"

	"github.com/creack/pty"
	"github.com/juli4n/ptyrpc/internal/api"
	"google.golang.org/grpc"
)

func main() {
	lis, err := net.Listen("tcp", "localhost:9876")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	var opts []grpc.ServerOption
	grpcServer := grpc.NewServer(opts...)
	api.RegisterPtyServiceServer(grpcServer, &server{})
	grpcServer.Serve(lis)
}

type server struct {
	api.UnimplementedPtyServiceServer
}

type streamWriter struct {
	stream api.PtyService_OnEventServer
}

func (s streamWriter) Write(p []byte) (n int, err error) {
	if err := s.stream.Send(&api.OnEventResponse{Buffer: p}); err != nil {
		return 0, err
	}
	return len(p), nil
}

var _ io.Writer = &streamWriter{}

func (s server) OnEvent(stream api.PtyService_OnEventServer) error {
	log.Printf("Starting session")
	c := exec.Command("/bin/bash")

	ptmx, err := pty.Start(c)
	if err != nil {
		return fmt.Errorf("could not create pty: %v", err)
	}
	defer func() { _ = ptmx.Close() }()

	waitSpawn := make(chan error)
	go func() {
		waitSpawn <- c.Wait()
	}()

	go func() {
		io.Copy(&streamWriter{stream}, ptmx)
	}()

	type streamMsg struct {
		req *api.OnEventRequest
		err error
	}
	msgChan := make(chan streamMsg)

	go func() {
		for {
			in, err := stream.Recv()
			msgChan <- streamMsg{req: in, err: err}
		}
	}()

	for {
		select {
		// The session subprocess finished. Finish the stream.
		case spawnErr := <-waitSpawn:
			log.Printf("Session subprocess finished, finishing the stream (err = %v)", spawnErr)
			return spawnErr
		// The session context was cancelled. Finish the session and the stream.
		case <-stream.Context().Done():
			log.Printf("Stream context cancelled, finishing the session")
			return c.Process.Kill()
		// There is a message from the client.
		case msg := <-msgChan:
			if msg.err != nil {
				log.Printf("Error from client, finishing session: %v", msg.err)
				return c.Process.Kill()
			}
			_, err = ptmx.Write(msg.req.Buffer)
			if err != nil {
				return err
			}
		}
	}
}

var _ api.PtyServiceServer = &server{}
