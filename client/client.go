package main

import (
	"context"
	"io"
	"log"
	"os"

	"github.com/juli4n/ptyrpc/internal/api"
	"golang.org/x/term"
	"google.golang.org/grpc"
)

func main() {
	ctx := context.Background()
	conn, err := grpc.Dial("localhost:9876", grpc.WithInsecure())
	if err != nil {
		log.Fatalf("could not dial to server: %v", err)
	}
	defer conn.Close()

	client := api.NewPtyServiceClient(conn)


	stream, err := client.OnEvent(ctx)
	if err != nil {
		log.Printf("could not connect to server: %v", err)
		return
	}
	defer stream.CloseSend()

	stdinState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		log.Printf("Failed to switch stdin to raw mode: %v", err)
		return
	}
	defer func() {
		_ = term.Restore(int(os.Stdin.Fd()), stdinState)
	}()

	waitc := make(chan struct{})
	go func() {
		for {
			in, err := stream.Recv()
			if err == io.EOF {
				close(waitc)
				return
			}
			if err != nil {
				log.Printf("failed to read from server: %v", err)
				close(waitc)
				return
			}
			os.Stdout.Write(in.Buffer)
		}
	}()

	_, err = io.Copy(streamWriter{stream}, os.Stdin)
	if err != nil && err != io.EOF {
		log.Fatalf("could not copy from stdin: %s", err)
	}
	<-waitc
}

type streamWriter struct {
	stream api.PtyService_OnEventClient
}

func (s streamWriter) Write(p []byte) (n int, err error) {
	if err := s.stream.Send(&api.OnEventRequest{Buffer: p}); err != nil {
		return 0, err
	}
	return len(p), nil
}
