package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"time"

	grpc_logrus "github.com/grpc-ecosystem/go-grpc-middleware/logging/logrus"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"

	pb "gitlab.alibaba-inc.com/moweng.xx/grpc-proxy/example"
)

type client struct {
	pb.ExampleClient
	logger *logrus.Entry
}

func main() {
	port := "8888"
	if len(os.Args) >= 2 {
		port = strings.TrimSpace(os.Args[1])
	}

	logger := logrus.StandardLogger().WithField("pid", os.Getpid())
	logger.Logger.Formatter = &logrus.JSONFormatter{TimestampFormat: "2006/01/02 15:04:05"}
	logger.Logger.SetLevel(logrus.DebugLevel)

	conn, err := grpc.Dial(
		"127.0.0.1:"+port, grpc.WithInsecure(),
		grpc.WithChainUnaryInterceptor(
			grpc_logrus.UnaryClientInterceptor(logger),
		),
		grpc.WithChainStreamInterceptor(
			grpc_logrus.StreamClientInterceptor(logger),
		),
	)
	if err != nil {
		logger.Fatalln(err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10000)
	defer cancel()
	cli := &client{
		ExampleClient: pb.NewExampleClient(conn),
		logger:        logger,
	}

	// cli.doUnaryReqEmpty(ctx)
	// cli.doUnaryNonEmpty(ctx)
	cli.doStreamRes(ctx)
}

func (c *client) doUnaryReqEmpty(ctx context.Context) {
	res, err := c.UnaryReqEmpty(ctx, &pb.EmptyRequest{})
	if err != nil {
		c.logger.Fatal(err)
	}
	s, _ := json.Marshal(res)
	c.logger.Infof(string(s))
}

func (c *client) doUnaryNonEmpty(ctx context.Context) {
	res, err := c.UnaryNonEmpty(ctx, &pb.HelloRequest{Req: "original message"})
	if err != nil {
		c.logger.Fatal(err)
	}
	s, _ := json.Marshal(res)
	c.logger.Infof(string(s))
}

func (c *client) doStreamRes(ctx context.Context) {
	cli, err := c.StreamRes(ctx, &pb.HelloRequest{Req: "original message"})
	if err != nil {
		c.logger.Fatal(err)
	}
	for {
		res, err := cli.Recv()
		if err != nil {
			if err != io.EOF {
				c.logger.Fatal(err)
			}
			break
		}
		s, _ := json.Marshal(res)
		c.logger.Info(string(s))
	}
}
