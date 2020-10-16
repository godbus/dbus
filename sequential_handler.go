package dbus

import (
	"container/list"
	"sync"
)

// NewSequentialSignalHandler returns an instance of a new
// signal handler that guarantees sequential processing of signals. It is a
// guarantee of this signal handler that signals will be written to
// channels in the order they are received on the DBus connection.
func NewSequentialSignalHandler() SignalHandler {
	return &sequentialSignalHandler{}
}

type sequentialSignalHandler struct {
	mu      sync.RWMutex
	closed  bool
	signals []*sequentialSignalChannelData
}

func (sh *sequentialSignalHandler) DeliverSignal(intf, name string, signal *Signal) {
	sh.mu.RLock()
	defer sh.mu.RUnlock()
	if sh.closed {
		return
	}
	for _, scd := range sh.signals {
		scd.deliver(signal)
	}
}

func (sh *sequentialSignalHandler) Terminate() {
	sh.mu.Lock()
	defer sh.mu.Unlock()
	if sh.closed {
		return
	}

	for _, scd := range sh.signals {
		scd.close()
		close(scd.ch)
	}
	sh.closed = true
	sh.signals = nil
}

func (sh *sequentialSignalHandler) AddSignal(ch chan<- *Signal) {
	sh.mu.Lock()
	defer sh.mu.Unlock()
	if sh.closed {
		return
	}
	sh.signals = append(sh.signals, newSequentialSignalChannelData(ch))
}

func (sh *sequentialSignalHandler) RemoveSignal(ch chan<- *Signal) {
	sh.mu.Lock()
	defer sh.mu.Unlock()
	if sh.closed {
		return
	}
	for i := len(sh.signals) - 1; i >= 0; i-- {
		if ch == sh.signals[i].ch {
			sh.signals[i].close()
			copy(sh.signals[i:], sh.signals[i+1:])
			sh.signals[len(sh.signals)-1] = nil
			sh.signals = sh.signals[:len(sh.signals)-1]
		}
	}
}

type sequentialSignalChannelData struct {
	ch   chan<- *Signal
	in   chan *Signal
	done chan struct{}
}

func newSequentialSignalChannelData(ch chan<- *Signal) *sequentialSignalChannelData {
	scd := &sequentialSignalChannelData{
		ch:   ch,
		in:   make(chan *Signal),
		done: make(chan struct{}),
	}
	go scd.bufferSignals()
	return scd
}

func (scd *sequentialSignalChannelData) bufferSignals() {
	var (
		queue list.List
		next  *Signal
	)
	defer close(scd.done)

	for {
		if next == nil {
			if queue.Len() != 0 {
				elem := queue.Front()
				queue.Remove(elem)
				next = elem.Value.(*Signal)
			} else {
				var ok bool
				next, ok = <-scd.in
				if !ok {
					return
				}
			}
		}
		select {
		case scd.ch <- next:
			// Signal delivered: the next signal will be
			// picked next iteration.
			next = nil
		case signal, ok := <-scd.in:
			if ok {
				queue.PushBack(signal)
			} else {
				return
			}
		}
	}
}

func (scd *sequentialSignalChannelData) deliver(signal *Signal) {
	scd.in <- signal
}

func (scd *sequentialSignalChannelData) close() {
	close(scd.in)
	// Ensure that bufferSignals() has exited and won't attempt
	// any future sends on scd.ch
	<-scd.done
}
