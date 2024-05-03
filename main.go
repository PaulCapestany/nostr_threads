package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"

	"github.com/couchbase/gocb/v2"
)

type Message struct {
	Content   string        `json:"content"`
	CreatedAt int64         `json:"created_at"`
	ID        string        `json:"id"`
	Kind      int           `json:"kind"`
	Pubkey    string        `json:"pubkey"`
	Sig       string        `json:"sig"`
	Tags      []interface{} `json:"tags"`
}

var cluster *gocb.Cluster
var allUniqueThreadMessages []Message

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("Usage: %s <messageID>", os.Args[0])
	}
	initialID := os.Args[1]
	messageIDsToQuery := []string{initialID}

	// Initialize Couchbase connection
	var err error
	cluster, err = gocb.Connect("couchbase://localhost", gocb.ClusterOptions{
		Username: "admin",
		Password: "hangman8june4magician9traverse8disbar4majolica4bacilli",
	})
	if err != nil {
		log.Fatalf("Could not connect to Couchbase: %v", err)
	}
	defer cluster.Close(nil)

	// Start fetching messages recursively
	messageFetcher(messageIDsToQuery)

	// Sort and output all unique messages by created_at
	sort.Slice(allUniqueThreadMessages, func(i, j int) bool {
		return allUniqueThreadMessages[i].CreatedAt < allUniqueThreadMessages[j].CreatedAt
	})

	output, err := json.Marshal(allUniqueThreadMessages)
	if err != nil {
		log.Fatalf("Failed to marshal messages: %v", err)
	}
	fmt.Println(string(output))
}

func messageFetcher(messageIDs []string) {
	for _, id := range messageIDs {
		query := fmt.Sprintf(`WITH referencedMessages AS (
			SELECT d.*
			FROM `+"`strfry-data`._default._default"+` AS d
			USE KEYS "%s"

			UNION

			SELECT refMessage.*
			FROM `+"`strfry-data`._default._default"+` AS refMessage
			USE INDEX (kind_and_event_lookup USING GSI)
			WHERE refMessage.kind = 1 AND (ANY t IN refMessage.tags SATISFIES t[0] = "e" AND t[1] = "%s" END)
		)
		SELECT message.content, message.created_at, message.id, message.kind, message.pubkey, message.sig, message.tags
		FROM referencedMessages AS message`, id, id)

		results, err := cluster.Query(query, nil)
		if err != nil {
			log.Printf("Failed to execute query for ID %s: %v", id, err)
			continue
		}

		var uniqueThreadMessages []Message
		for results.Next() {
			var msg Message
			if err := results.Row(&msg); err != nil {
				log.Printf("Failed to parse message: %v", err)
				continue
			}
			if msg.Kind != 1 || containsMessage(allUniqueThreadMessages, msg.ID) {
				continue
			}
			uniqueThreadMessages = append(uniqueThreadMessages, msg)
			allUniqueThreadMessages = append(allUniqueThreadMessages, msg)
		}

		if err := results.Err(); err != nil {
			log.Printf("Error iterating results: %v", err)
		}

		// Extract and process tags
		var messageIDsToQuery []string
		for _, msg := range uniqueThreadMessages {
			for _, tag := range msg.Tags {
				tagSlice, ok := tag.([]interface{})
				if !ok || len(tagSlice) < 2 || tagSlice[0] != "e" {
					continue
				}
				if idStr, ok := tagSlice[1].(string); ok && !containsMessage(allUniqueThreadMessages, idStr) {
					messageIDsToQuery = append(messageIDsToQuery, idStr)
				}
			}
		}
		if len(messageIDsToQuery) > 0 {
			messageFetcher(messageIDsToQuery)
		}
	}
}

func containsMessage(messages []Message, id string) bool {
	for _, msg := range messages {
		if msg.ID == id {
			return true
		}
	}
	return false
}
