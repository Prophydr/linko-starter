package main

import (
	"io"
	"net/http"
)

type spyReadCloser struct {
	io.ReadCloser
	requestBodyBytes int
}

func (r *spyReadCloser) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	r.requestBodyBytes += n
	return n, err
}

type spyResponseWriter struct {
	http.ResponseWriter
	responseStatus    int
	responseBodyBytes int
}

func (w *spyResponseWriter) Write(p []byte) (int, error) {
	if w.responseStatus == 0 {
		w.responseStatus = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(p)
	w.responseBodyBytes += n
	return n, err
}

func (w *spyResponseWriter) WriteHeader(statusCode int) {
	w.responseStatus = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}
