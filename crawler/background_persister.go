package crawler

import (
	"fmt"
	"hash/fnv"
	"sync"
)

// NewBackgroundFilePersister wraps a FilePersister with async worker pool.
func NewBackgroundFilePersister(delegate FilePersister, workerCount, bufferSize int, logger Logger) FilePersister {
	if workerCount <= 0 {
		workerCount = 1
	}
	if bufferSize <= 0 {
		bufferSize = 1
	}
	perWorker := bufferSize / workerCount
	if perWorker <= 0 {
		perWorker = 1
	}
	p := &backgroundFilePersister{
		delegate: delegate,
		queues:   make([]chan saveTask, 0, workerCount),
		logger:   logger,
	}
	p.wg.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		queue := make(chan saveTask, perWorker)
		p.queues = append(p.queues, queue)
		go p.worker(queue)
	}
	return p
}

type backgroundFilePersister struct {
	delegate FilePersister
	queues   []chan saveTask
	wg       sync.WaitGroup
	logger   Logger
	mu       sync.RWMutex
	closed   bool
}

type saveTask struct {
	targetID string
	fileName string
	content  []byte
}

func (p *backgroundFilePersister) worker(queue <-chan saveTask) {
	defer p.wg.Done()
	for task := range queue {
		if err := p.delegate.Save(task.targetID, task.fileName, task.content); err != nil {
			if p.logger != nil {
				p.logger.Error("Background persistence failed for %s/%s: %v", task.targetID, task.fileName, err)
			}
		}
	}
}

func (p *backgroundFilePersister) Save(targetID, fileName string, content []byte) error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.closed {
		return fmt.Errorf("persister is closed")
	}
	queue := p.queueFor(targetID, fileName)
	queue <- saveTask{targetID: targetID, fileName: fileName, content: append([]byte(nil), content...)}
	return nil
}

func (p *backgroundFilePersister) queueFor(targetID, fileName string) chan saveTask {
	if len(p.queues) <= 1 {
		return p.queues[0]
	}
	h := fnv.New32a()
	h.Write([]byte(targetID))
	h.Write([]byte{0})
	h.Write([]byte(fileName))
	return p.queues[int(h.Sum32()%uint32(len(p.queues)))]
}

func (p *backgroundFilePersister) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	for _, q := range p.queues {
		close(q)
	}
	p.mu.Unlock()
	p.wg.Wait()
	return p.delegate.Close()
}
