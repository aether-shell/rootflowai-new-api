package service

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
)

func TestIOCopyBytesGracefullyStoresClientWriteError(t *testing.T) {
	c := &gin.Context{}
	c.Writer = &failingResponseWriter{
		header: http.Header{},
		err:    errors.New("write tcp: broken pipe"),
	}

	err := IOCopyBytesGracefully(c, &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}, []byte(`{"ok":true}`))

	if err == nil {
		t.Fatal("expected write error")
	}
	got := common.GetContextKeyString(c, constant.ContextKeyClientResponseWriteError)
	if got != "write tcp: broken pipe" {
		t.Fatalf("context write error = %q", got)
	}
}

type failingResponseWriter struct {
	header http.Header
	status int
	size   int
	err    error
}

func (w *failingResponseWriter) Header() http.Header {
	return w.header
}

func (w *failingResponseWriter) Write([]byte) (int, error) {
	return 0, w.err
}

func (w *failingResponseWriter) WriteHeader(statusCode int) {
	w.status = statusCode
}

func (w *failingResponseWriter) WriteHeaderNow() {
	if w.status == 0 {
		w.status = http.StatusOK
	}
}

func (w *failingResponseWriter) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

func (w *failingResponseWriter) Status() int {
	return w.status
}

func (w *failingResponseWriter) Size() int {
	return w.size
}

func (w *failingResponseWriter) Written() bool {
	return w.status != 0
}

func (w *failingResponseWriter) Flush() {}

func (w *failingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, errors.New("hijack unsupported")
}

func (w *failingResponseWriter) CloseNotify() <-chan bool {
	ch := make(chan bool)
	return ch
}

func (w *failingResponseWriter) Pusher() http.Pusher {
	return nil
}
