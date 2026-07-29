package handler

import (
	"context"
	"sync"
	"testing"
)

func TestCanvasCollaborationHubDisconnectsSlowClientWithoutClosingSendQueue(t *testing.T) {
	hub := &canvasCollaborationHub{clients: map[string]map[*canvasRealtimeClient]struct{}{}}
	ctx, cancel := context.WithCancel(context.Background())
	client := &canvasRealtimeClient{
		canvasID: "canvas-a", connectionID: "connection-a",
		send: make(chan []byte, 1), cancel: cancel,
	}
	hub.register(client)
	client.send <- []byte("occupied")
	hub.broadcast(client.canvasID, []byte("next"))

	select {
	case <-ctx.Done():
	default:
		t.Fatal("slow client context was not cancelled")
	}
	hub.mu.RLock()
	_, exists := hub.clients[client.canvasID]
	hub.mu.RUnlock()
	if exists {
		t.Fatal("slow client room still registered")
	}

	// A broadcast that already copied the client may still enqueue after
	// unregister. The queue intentionally remains open so this cannot panic.
	select {
	case client.send <- []byte("late"):
	default:
	}
}

func TestCanvasCollaborationHubConcurrentBroadcastAndUnregister(t *testing.T) {
	hub := &canvasCollaborationHub{clients: map[string]map[*canvasRealtimeClient]struct{}{}}
	const clientCount = 24
	clients := make([]*canvasRealtimeClient, 0, clientCount)
	for index := 0; index < clientCount; index++ {
		_, cancel := context.WithCancel(context.Background())
		client := &canvasRealtimeClient{
			canvasID: "canvas-concurrent", connectionID: secureTestConnectionID(index),
			send: make(chan []byte, 128), cancel: cancel,
		}
		clients = append(clients, client)
		hub.register(client)
	}

	var wait sync.WaitGroup
	for index := 0; index < 50; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			hub.broadcast("canvas-concurrent", []byte(`{"type":"presence"}`))
		}()
	}
	for _, client := range clients {
		wait.Add(1)
		go func(target *canvasRealtimeClient) {
			defer wait.Done()
			hub.unregister(target)
		}(client)
	}
	wait.Wait()
}

func secureTestConnectionID(index int) string {
	const digits = "0123456789abcdefghijklmnopqrstuvwxyz"
	if index < len(digits) {
		return "connection-" + string(digits[index])
	}
	return "connection-overflow"
}
