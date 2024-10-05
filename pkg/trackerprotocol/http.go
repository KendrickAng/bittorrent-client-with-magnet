package trackerprotocol

import (
	"context"
	"errors"
	"example.com/btclient/pkg/bencodeutil"
	"example.com/btclient/pkg/closelogger"
	"fmt"
	"golang.org/x/exp/rand"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
)

const (
	startPort = 6881
	endPort   = 6889
)

func (h *Handler) handleHttp(ctx context.Context) error {
	// Reserve port for this application
	port, err := h.reservePort()
	if err != nil {
		return err
	}
	println("port reserved", port)

	// Generate a random peer ID
	peerID, err := random20Bytes()
	if err != nil {
		return err
	}
	println("generated peer id")

	// Build GET request to tracker
	trackerURL, err := h.buildTrackerURL(peerID, port)
	if err != nil {
		return err
	}
	println("generated tracker url", trackerURL)

	// Send GET request to tracker
	resp, err := http.Get(trackerURL)
	if err != nil {
		return err
	}
	defer closelogger.CloseOrLog(resp.Body, "Tracker GET response body")

	// Retrieve tracker response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	println("received tracker response")

	// Parse tracker response
	trackerResp, err := bencodeutil.UnmarshalTrackerResponse(body)
	if err != nil {
		return err
	}
	if len(trackerResp.Peers) == 0 {
		return errors.New("no peers found")
	}
	println("parsed tracker response")

	// Connect to available peers
	clientsCh := make(chan *Client, len(trackerResp.Peers))
	println("attempting connection to", len(trackerResp.Peers), "peers")
	wg := new(sync.WaitGroup)
	for _, peer := range trackerResp.Peers {
		wg.Add(1)

		go func(peer2 bencodeutil.Peer) {
			defer wg.Done()

			// dial peer
			conn, err := net.DialTimeout("tcp", peer.String(), peerDialTimeout)
			if err != nil {
				println("error creating client for peer", peer2.String())
				return
			}
			println("dialed", conn.RemoteAddr().String())

			// create client to peer
			client := NewClient(conn, conn, NewHandshaker(conn), peerID, h.torrent.InfoHash)
			if err := client.Init(); err != nil {
				println("error creating client for peer", peer2.String())
				return
			}

			clientsCh <- client
			fmt.Printf("created client for %s\n", peer2.String())
		}(peer)
	}
	wg.Wait()
	close(clientsCh) // close channel so we don't loop over it infinitely

	// Convert peers channel into peers queue
	var clients []*Client
	for client := range clientsCh {
		clients = append(clients, client)
	}
	fmt.Printf("found %d peers\n", len(clients))

	// Start downloading
	manager, err := NewDownloadManager(h.torrent, clients)
	if err != nil {
		return err
	}
	fmt.Println("starting download")

	if err := manager.Start(ctx); err != nil {
		return err
	}
	fmt.Println("download completed")

	return nil
}

func (h *Handler) reservePort() (int, error) {
	for port := startPort; port <= endPort; port++ {
		listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err == nil {
			h.httpListener = listener
			return port, nil
		}
	}
	return -1, errors.New("could not find free port")
}

func (h *Handler) buildTrackerURL(peerID [20]byte, port int) (string, error) {
	base, err := h.announceUrl.Parse(h.torrent.Announce)
	if err != nil {
		return "", err
	}
	params := url.Values{
		"info_hash":  []string{string(h.torrent.InfoHash[:])},
		"peer_id":    []string{string(peerID[:])},
		"port":       []string{strconv.Itoa(port)},
		"uploaded":   []string{"0"},
		"downloaded": []string{"0"},
		"compact":    []string{"1"},
		"left":       []string{strconv.Itoa(h.torrent.Length)},
	}
	base.RawQuery = params.Encode()
	return base.String(), nil
}

func random20Bytes() ([20]byte, error) {
	var bb [20]byte

	b, err := randomBytes(20)
	if err != nil {
		return bb, err
	}

	copy(bb[:], b)
	return bb, nil
}

func randomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}
