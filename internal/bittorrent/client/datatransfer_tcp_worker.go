package client

import (
	"context"
	"errors"
	"example.com/btclient/internal/bittorrent"
	"example.com/btclient/internal/bittorrent/message"
	"example.com/btclient/internal/bittorrent/peer"
	"math"
)

const (
	maxRequestLength = 16384 // 2 ^ 14 (16kiB)
)

type pieceRequest struct {
	// Index identifying the piece to download.
	pieceIndex int
	// Size of a piece, in bytes.
	pieceLength int
	// Bytes to download in a single request message, in bytes.
	requestLength     int
	expectedPieceHash [20]byte
}

type pieceResult struct {
	piece []byte
	index int
	hash  [20]byte
}

// downloadWorker handles the download of a single piece of datareader in the torrent.
// A torrent is split into many pieces for download.
type downloadWorker struct {
	client *peer.Client
}

func newDownloadWorker(client *peer.Client) *downloadWorker {
	return &downloadWorker{
		client: client,
	}
}

// start starts the worker downloading available pieces from a client.
func (d *downloadWorker) start(ctx context.Context, req pieceRequest) (*pieceResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// If choked, try to unchoke ourselves
	if d.client.IsChoked() {
		if err := d.client.SendInterestedMessage(); err != nil {
			return nil, err
		}
		if _, err := d.client.ReceiveUnchokeMessage(); err != nil {
			return nil, err
		}
		d.client.SetChoked(false)
		println(d.client.String(), "unchoked")
	}

	remainingBytes := req.pieceLength
	numRequests := int(math.Ceil(float64(req.pieceLength) / float64(req.requestLength)))
	index := uint32(req.pieceIndex)
	blocks := make([][]byte, numRequests)

	// number of 'request' messages to send to download a single piece
	for i := 0; i < numRequests; i++ {
		begin := uint32(req.requestLength * i)
		reqLength := uint32(min(req.requestLength, remainingBytes))

		// send a 'request' message with the goal of getting a 'piece' message
		if err := d.client.SendRequestMessage(index, begin, reqLength); err != nil {
			return nil, err
		}

		// keep receiving messages until we get a 'piece' message
	Inner:
		for {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
				msg, err := d.client.ReceiveMessage()
				if err != nil {
					return nil, err
				}
				switch msg.ID {
				case message.MsgKeepAlive:
					println("keep alive")
				case message.MsgChoke:
					d.client.SetChoked(true)
				case message.MsgUnchoke:
					d.client.SetChoked(false)
				case message.MsgBitfield:
					d.client.SetBitfield(msg.AsMsgBitfield().Bitfield)
				case message.MsgPiece:
					piece := msg.AsMsgPiece()
					println("piece", index, ":", begin, "to", begin+uint32(len(piece.Block)), "of total", req.pieceLength)
					if piece.Begin != begin || piece.Index != index {
						return nil, errors.New("invalid piece")
					}
					blocks[i] = piece.Block
					remainingBytes -= len(piece.Block)
					break Inner
				}
			}
		}
	}

	// re-create the final piece from all the 'piece' messages
	var finalBlocks []byte
	for _, block := range blocks {
		finalBlocks = append(finalBlocks, block...)
	}

	return &pieceResult{
		piece: finalBlocks,
		index: req.pieceIndex,
		hash:  bittorrent.Hash(finalBlocks),
	}, nil
}
