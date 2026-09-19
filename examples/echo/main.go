// Command echo is a two-role demo: host prints a QR URI, device pastes it.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"

	"github.com/LubyRuffy/pairlink/client"
	"github.com/LubyRuffy/pairlink/crypto"
	"github.com/LubyRuffy/pairlink/protocol"
	"github.com/LubyRuffy/pairlink/qr"
)

func main() {
	role := flag.String("role", "host", "host or device")
	hub := flag.String("hub", "", "hub base URL (http://127.0.0.1:7780)")
	token := flag.String("token", "", "host token (host role)")
	offer := flag.String("offer", "", "scanned pairlink:v1 URI (device role)")
	flag.Parse()
	if *hub == "" {
		log.Fatal("-hub is required")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	id, err := crypto.Generate()
	if err != nil {
		log.Fatal(err)
	}
	switch *role {
	case "host":
		if *token == "" {
			log.Fatal("-token is required")
		}
		if err := client.RegisterHost(ctx, *hub, *token, id); err != nil {
			log.Fatal(err)
		}
		c, err := client.Dial(ctx, client.Config{HubURL: *hub, Identity: id, Token: *token})
		if err != nil {
			log.Fatal(err)
		}
		defer c.Close()
		c.OnLink(func(l *client.Link) {
			go func() {
				for msg := range l.Recv() {
					fmt.Println("recv", string(msg), "path="+l.Path())
					_ = l.Send(msg)
				}
			}()
		})
		uri, pairingID, err := c.CreateOffer(ctx)
		if err != nil {
			log.Fatal(err)
		}
		png, err := qr.PNG(uri, 256)
		if err != nil {
			log.Fatal(err)
		}
		_ = os.WriteFile("pairlink-offer.png", png, 0o600)
		fmt.Println("pairing_id", pairingID)
		fmt.Println(uri)
		fmt.Println("wrote pairlink-offer.png")
		<-ctx.Done()
	case "device":
		if *offer == "" {
			log.Fatal("-offer is required")
		}
		o, err := protocol.Parse(*offer)
		if err != nil {
			log.Fatal(err)
		}
		ticket, hostPub, sid, err := client.RedeemOffer(ctx, o, id)
		if err != nil {
			log.Fatal(err)
		}
		c, err := client.Dial(ctx, client.Config{HubURL: o.HubURL, Identity: id, Token: ticket})
		if err != nil {
			log.Fatal(err)
		}
		defer c.Close()
		link, err := c.OpenInitiator(ctx, hostPub, sid)
		if err != nil {
			log.Fatal(err)
		}
		if err := link.Send([]byte("echo")); err != nil {
			log.Fatal(err)
		}
		fmt.Println("sent", "path="+link.Path())
	default:
		log.Fatal("role host|device")
	}
}
