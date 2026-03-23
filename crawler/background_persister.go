package crawler

import (
	"fmt"
	"hash/fnv"
	"sync"
)

type backgroundFilePersister struct {
	delegate FilePersister
	queues   []chan saveTask
	wg       sync.WaitGroup
	logger   Logger
	mu       sync.RWMutex
	closed   bool
}

type saveTask struct {
	productID string
	fileName  string
	content   []byte
}

func newBackgroundFilePersister(delegate FilePersister, workerCount int, bufferSize int, logger Logger) FilePersister {
	if workerCount <= 0 {
		workerCount = 1
	}
	if bufferSize <= 0 {
		bufferSize = 1
	}
	perWorkerBufferSize := bufferSize / workerCount
	if perWorkerBufferSize <= 0 {
		perWorkerBufferSize = 1
	}
	p := &backgroundFilePersister{
		delegate: delegate,
		queues:   make([]chan saveTask, 0, workerCount),
		logger:   logger,
	}
	p.wg.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		queue := make(chan saveTask, perWorkerBufferSize)
		p.queues = append(p.queues, queue)
		go p.worker(queue)
	}
	return p
}

func (p *backgroundFilePersister) worker(queue <-chan saveTask) {
	defer p.wg.Done()
	for task := range queue {
		if err := p.delegate.Save(task.productID, task.fileName, task.content); err != nil {
			if p.logger != nil {
				p.logger.Error("Background persistence failed for %s/%s: %v", task.productID, task.fileName, err)
			}
		}
	}
}

func (p *backgroundFilePersister) Save(productID, fileName string, content []byte) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return fmt.Errorf("persister is closed")
	}

	queue := p.queueFor(productID, fileName)
	queue <- saveTask{
		productID: productID,
		fileName:  fileName,
		content:   append([]byte(nil), content...),
	}
	return nil
}

func (p *backgroundFilePersister) queueFor(productID, fileName string) chan saveTask {
	if len(p.queues) == 0 {
		return nil
	}
	if len(p.queues) == 1 {
		return p.queues[0]
	}
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(productID))
	_, _ = hasher.Write([]byte{0})
	_, _ = hasher.Write([]byte(fileName))
	index := int(hasher.Sum32() % uint32(len(p.queues)))
	return p.queues[index]
}

func (p *backgroundFilePersister) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	for _, queue := range p.queues {
		close(queue)
	}
	p.mu.Unlock()

	p.wg.Wait()
	return p.delegate.Close()
}
