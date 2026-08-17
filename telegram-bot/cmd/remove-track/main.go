// One-shot utility: removes a video from the bot's club queue on the relay.
// Usage: BOT_NSEC=... BOT_CLUB_ID=... REMOVE_VID=kPMRkQK2szI ./remove-track
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip19"
)

func main() {
	nsec := os.Getenv("BOT_NSEC")
	clubID := os.Getenv("BOT_CLUB_ID")
	removeVid := os.Getenv("REMOVE_VID")
	relayURL := os.Getenv("RELAY_URL")
	if relayURL == "" {
		relayURL = "wss://relay.zapclub.io"
	}
	if nsec == "" || clubID == "" || removeVid == "" {
		log.Fatal("need BOT_NSEC, BOT_CLUB_ID, REMOVE_VID")
	}

	_, sk, err := nip19.Decode(nsec)
	if err != nil {
		log.Fatalf("decode nsec: %v", err)
	}
	skHex := sk.(string)
	pk, _ := nostr.GetPublicKey(skHex)
	log.Printf("bot pk: %.8s... club: %s remove: %s", pk, clubID, removeVid)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	relay, err := nostr.RelayConnect(ctx, relayURL)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer relay.Close()

	// NIP-42 AUTH
	time.Sleep(2 * time.Second)
	if err := relay.Auth(ctx, func(ev *nostr.Event) error {
		ev.PubKey = pk
		return ev.Sign(skHex)
	}); err != nil {
		log.Printf("auth err (continuing): %v", err)
	}

	// Fetch both bot-authored and relay-managed 30103 events
	sub, err := relay.Subscribe(ctx, []nostr.Filter{
		{Kinds: []int{30103}, Authors: []string{pk}, Tags: nostr.TagMap{"h": {clubID}, "d": {clubID}}, Limit: 1},
		{Kinds: []int{30103}, Tags: nostr.TagMap{"h": {clubID}, "d": {pk + ":" + clubID}}, Limit: 1},
	})
	if err != nil {
		log.Fatalf("subscribe: %v", err)
	}
	defer sub.Unsub()

	var best *nostr.Event
drain:
	for {
		select {
		case ev := <-sub.Events:
			if ev != nil && (best == nil || ev.CreatedAt > best.CreatedAt) {
				best = ev
				log.Printf("found event: pk=%.8s d=%s ts=%d tracks=%d",
					ev.PubKey, tagVal(ev, "d"), int64(ev.CreatedAt),
					len(trackTags(ev)))
			}
		case <-sub.EndOfStoredEvents:
			break drain
		case <-ctx.Done():
			break drain
		}
	}

	if best == nil {
		log.Fatal("no queue event found")
	}

	// Rebuild tags: remove the target video, drop off-tracks, deduplicate.
	newTags := nostr.Tags{{"h", clubID}, {"d", clubID}}
	seen := map[string]bool{}
	removed := false
	for _, tag := range best.Tags {
		if len(tag) < 2 || tag[0] != "track" {
			continue
		}
		vid := strings.TrimPrefix(tag[1], "yt:")
		if vid == removeVid {
			removed = true
			log.Printf("removing: %s (%s)", vid, tag[2])
			continue
		}
		if len(tag) >= 5 && tag[4] == "off" {
			log.Printf("skipping off track: %s", vid)
			continue
		}
		if seen[vid] {
			log.Printf("deduplicating: %s", vid)
			continue
		}
		seen[vid] = true
		newTags = append(newTags, tag)
	}
	if !removed {
		// Not an error if track was already gone — still publish deduplicated queue.
		log.Printf("note: video %s not in queue (may already be gone) — publishing deduped queue", removeVid)
	}
	trackCount := len(newTags) - 2 // subtract h and d tags
	log.Printf("publishing new queue: %d tracks", trackCount)

	newEv := nostr.Event{
		PubKey:    pk,
		Kind:      30103,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Tags:      newTags,
		Content:   "",
	}
	_ = newEv.Sign(skHex)
	if err := relay.Publish(ctx, newEv); err != nil {
		log.Fatalf("publish: %v", err)
	}
	fmt.Printf("✅ Published queue without %s (%d tracks remaining)\n", removeVid, trackCount)
}

func tagVal(ev *nostr.Event, key string) string {
	for _, t := range ev.Tags {
		if len(t) >= 2 && t[0] == key {
			return t[1]
		}
	}
	return ""
}

func trackTags(ev *nostr.Event) []nostr.Tag {
	var out []nostr.Tag
	for _, t := range ev.Tags {
		if len(t) >= 2 && t[0] == "track" {
			out = append(out, t)
		}
	}
	return out
}

var _ = strconv.Itoa
