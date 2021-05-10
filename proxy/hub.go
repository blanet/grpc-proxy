package proxy

import (
	"context"
	"errors"
	"io"

	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type StreamHub struct {
	pioneer        []byte
	chanFullDuplex cable   // the stream that will be proxied back to user
	chanHalfDuplex []cable // streams that will not be proxied to user

	cancels []func()
}

// cable wraps the grpc.ClientStream between proxy and backend server. We use field
// `userFaced` to distinguish whether the cable will be finally streamed to user.
// If false, all proto messages received from backend server should be swallowed.
type cable struct {
	grpc.ClientStream
	cancel func()
}

func newStreamHub(method string, dst *StreamDestination) (*StreamHub, error) {
	if dst == nil || len(dst.replications) == 0 {
		return nil, status.Errorf(codes.Unavailable, "no replications for proxying")
	}

	res := &StreamHub{
		pioneer: dst.pioneer,
	}
	for _, replica := range dst.replications {
		ctx, cancel := context.WithCancel(replica.Ctx)
		res.cancels = append(res.cancels, cancel)
		downstream, err := grpc.NewClientStream(ctx, proxyStreamDesc, replica.Conn, method)
		if err != nil {
			return nil, err
		}
		if replica.UserOriented {
			res.chanFullDuplex = cable{
				ClientStream: downstream,
				cancel:       cancel,
			}
		} else {
			res.chanHalfDuplex = append(res.chanHalfDuplex, cable{
				ClientStream: downstream,
				cancel:       cancel,
			})
		}
	}

	return res, nil
}

func (h *StreamHub) cancel() {
	for _, cancel := range h.cancels {
		cancel()
	}
}

// proxyToNobody: receive from backend server(s) and do nothing(drop received messages)
func (h *StreamHub) proxyToNobody() chan error {
	ret := make(chan error, 1)
	go func() {
		var g errgroup.Group
		for _, src := range h.chanHalfDuplex {
			src := src
			g.Go(func() error {
				for i := 0; ; i++ {
					f := &frame{}
					if err := src.RecvMsg(f); err != nil {
						if errors.Is(err, io.EOF) {
							return nil
						}

						src.cancel()
						return err
					}
				}
			})
		}

		ret <- g.Wait()
	}()
	return ret
}

func (h *StreamHub) proxyToClient(target grpc.ServerStream) chan error {
	ret := make(chan error, 1)

	go func() {
		var outerErr error
		f := &frame{}

		for i := 0; ; i++ {
			if err := h.chanFullDuplex.RecvMsg(f); err != nil {
				outerErr = err // this can be io.EOF which is happy case
				break
			}
			if i == 0 {
				// This is a bit of a hack, but client to server headers are only readable after first client msg is
				// received but must be written to server stream before the first msg is flushed.
				// This is the only place to do it nicely.
				md, err := h.chanFullDuplex.Header()
				if err != nil {
					outerErr = err
					break
				}
				if err := target.SendHeader(md); err != nil {
					outerErr = err
					break
				}
			}

			if err := target.SendMsg(f); err != nil {
				outerErr = err
				break
			}
		}

		ret <- outerErr
	}()

	return ret
}

// proxyToServer: receive from client and send to backend server(s)
func (h *StreamHub) proxyToServer(source grpc.ServerStream) chan error {
	allChans := append(h.chanHalfDuplex, h.chanFullDuplex)
	ret := make(chan error, 1)
	go func() {
		var g errgroup.Group

		frameChans := make([]chan<- *frame, 0, len(allChans))

		for _, dst := range allChans {
			dst := dst
			frameChan := make(chan *frame, 16)
			frameChan <- &frame{payload: h.pioneer} // send re-written message
			frameChans = append(frameChans, frameChan)

			g.Go(func() error {
				for f := range frameChan {
					if err := dst.SendMsg(f); err != nil {
						if errors.Is(err, io.EOF) {
							break
						}
						return err
					}
				}
				// all messages redirected
				return dst.CloseSend()
			})
		}

		var outerErr error
		for {
			f := &frame{}
			if err := source.RecvMsg(f); err != nil {
				for _, frameChan := range frameChans {
					// signal no more data to redirect
					close(frameChan)
				}
				if !errors.Is(err, io.EOF) {
					outerErr = err
				}
				break
			}
			for _, frameChan := range frameChans {
				frameChan <- f
			}
		}

		if err := g.Wait(); outerErr == nil {
			outerErr = err
		}

		ret <- outerErr
	}()

	return ret
}

func (h *StreamHub) setTrailer(target grpc.ServerStream) {
	target.SetTrailer(h.chanFullDuplex.Trailer())
}
