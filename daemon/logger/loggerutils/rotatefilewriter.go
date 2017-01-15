package loggerutils

import (
	"io"
	"os"
	"strconv"
	"sync"

	"github.com/docker/docker/pkg/archive"
	"github.com/docker/docker/pkg/pubsub"
)

// RotateFileWriter is Logger implementation for default Docker logging.
type RotateFileWriter struct {
	f            io.WriteCloser // store for closing
	logName      string
	mu           sync.Mutex
	capacity     int64 //maximum size of each file
	currentSize  int64 // current size of the latest file
	maxFiles     int   //maximum number of files
	notifyRotate *pubsub.Publisher
}

//NewRotateFileWriter creates new RotateFileWriter
func NewRotateFileWriter(logPath string, capacity int64, maxFiles int) (*RotateFileWriter, error) {
	log, err := os.OpenFile(logPath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0640)
	if err != nil {
		return nil, err
	}

	size, err := log.Seek(0, os.SEEK_END)
	if err != nil {
		return nil, err
	}

	wc, err := archive.CompressStream(log, archive.Gzip)
	if err != nil {
		return nil, err
	}

	return &RotateFileWriter{
		f:            wc,
		logName:      log.Name(),
		capacity:     capacity,
		currentSize:  size,
		maxFiles:     maxFiles,
		notifyRotate: pubsub.NewPublisher(0, 1),
	}, nil
}

//WriteLog write log message to File
func (w *RotateFileWriter) Write(message []byte) (int, error) {
	w.mu.Lock()
	if err := w.checkCapacityAndRotate(); err != nil {
		w.mu.Unlock()
		return -1, err
	}

	n, err := w.f.Write(message)
	//	n, err := writeCloser.Write(message)
	if err == nil {
		w.currentSize += int64(n)
	}
	w.mu.Unlock()
	return n, err
}

func (w *RotateFileWriter) checkCapacityAndRotate() error {
	if w.capacity == -1 {
		return nil
	}

	if w.currentSize >= w.capacity {
		name := w.logName
		if err := w.f.Close(); err != nil {
			return err
		}
		if err := rotate(name, w.maxFiles); err != nil {
			return err
		}
		file, err := os.OpenFile(name, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 06400)
		if err != nil {
			return err
		}

		wc, err := archive.CompressStream(file, archive.Gzip)
		if err != nil {
			return err
		}

		w.f = wc
		w.currentSize = 0
		w.notifyRotate.Publish(struct{}{})
	}

	return nil
}

func rotate(name string, maxFiles int) error {
	if maxFiles < 2 {
		return nil
	}
	for i := maxFiles - 1; i > 1; i-- {
		toPath := name + "." + strconv.Itoa(i)
		fromPath := name + "." + strconv.Itoa(i-1)
		if err := os.Rename(fromPath, toPath); err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	if err := os.Rename(name, name+".1"); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// LogPath returns the location the given writer logs to.
func (w *RotateFileWriter) LogPath() string {
	return w.logName
}

// MaxFiles return maximum number of files
func (w *RotateFileWriter) MaxFiles() int {
	return w.maxFiles
}

//NotifyRotate returns the new subscriber
func (w *RotateFileWriter) NotifyRotate() chan interface{} {
	return w.notifyRotate.Subscribe()
}

//NotifyRotateEvict removes the specified subscriber from receiving any more messages.
func (w *RotateFileWriter) NotifyRotateEvict(sub chan interface{}) {
	w.notifyRotate.Evict(sub)
}

// Close closes underlying file and signals all readers to stop.
func (w *RotateFileWriter) Close() error {
	return w.f.Close()
}
