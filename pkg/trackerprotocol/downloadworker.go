package trackerprotocol

import (
	"context"
	"errors"
	"math"
)

type DownloadWorker struct {
	client   *Client
	requests chan *pieceRequest
	results  chan *pieceResult
}

func NewDownloadWorker(client *Client, requests chan *pieceRequest, results chan *pieceResult) *DownloadWorker {
	return &DownloadWorker{
		client:   client,
		requests: requests,
		results:  results,
	}
}

// Start starts the worker downloading available pieces from a client.
func (d *DownloadWorker) Start(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case req := <-d.requests:
			// If choked, try to unchoke ourselves
			if d.client.IsChoked() {
				if err := d.client.SendInterestedMessage(); err != nil {
					d.handleError(err, req)
					continue
				}
				if _, err := d.client.ReceiveUnchokeMessage(); err != nil {
					d.handleError(err, req)
					continue
				}
				println(d.client.String(), "unchoked")
			}

			// Unchoked, start downloading pieces
			numRequests := int(math.Ceil(float64(req.pieceLength) / float64(req.requestLength)))
			blocks := make([][]byte, numRequests)
			for i := 0; i < numRequests; i++ {
				// Last request may be smaller than the others
				if i == numRequests-1 {
					// TODO
				}
				index := uint32(req.pieceLength)
				begin := uint32(req.requestLength * i)
				length := uint32(req.pieceLength)
				if err := d.client.SendRequestMessage(index, begin, length); err != nil {
					d.handleError(err, req)
					continue
				}
				pieceMsg, err := d.client.ReceivePieceMessage()
				if err != nil {
					d.handleError(err, req)
					continue
				}
				println(d.client.String(), " received piece message")
				if pieceMsg.Begin != begin || pieceMsg.Index != index {
					d.handleError(errors.New("invalid piece"), req)
					continue
				}
				blocks[i] = pieceMsg.Block
			}

			// TODO merge the blocks

			d.results <- &pieceResult{} // TODO complete

			return nil
		}
	}
}

func (d *DownloadWorker) handleError(err error, req *pieceRequest) {
	println("worker ", d.client.String(), " error ", err.Error())
	d.requests <- req
}
