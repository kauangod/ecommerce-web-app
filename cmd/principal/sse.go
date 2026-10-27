package main

import "sync"

type Conexoes struct {
	mu              sync.Mutex
	client_channels map[string][]chan string
}

func NovasConexoes() *Conexoes {
	return &Conexoes{client_channels: map[string][]chan string{}}
}

func (c *Conexoes) Abrir(email string) chan string {
	ch := make(chan string, 16)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.client_channels[email] = append(c.client_channels[email], ch)
	return ch
}

func (c *Conexoes) Fechar(email string, ch chan string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, current := range c.client_channels[email] {
		if current == ch {
			c.client_channels[email] = append(c.client_channels[email][:i], c.client_channels[email][i+1:]...)
			close(ch)
			break
		}
	}
}

func (c *Conexoes) Notificar(email string, msg string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, ch := range c.client_channels[email] {
		select {
		case ch <- msg:
		default:
		}
	}
}
