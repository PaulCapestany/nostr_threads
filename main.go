package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/couchbase/gocb/v2"
)

type Message struct {
	Content   string     `json:"content"`
	CreatedAt int64      `json:"created_at"`
	ID        string     `json:"id"`
	Kind      int        `json:"kind"`
	Pubkey    string     `json:"pubkey"`
	Sig       string     `json:"sig"`
	Tags      [][]string `json:"tags"`
}

var allUniqueThreadMessages []Message

func main() {
	if len(os.Args) != 2 {
		fmt.Println("Usage: nostr_threads <message_id>")
		os.Exit(1)
	}

	messageID := os.Args[1]

	cluster, err := gocb.Connect("couchbase://localhost", gocb.ClusterOptions{
		Username: "admin",
		Password: "hangman8june4magician9traverse8disbar4majolica4bacilli",
	})
	if err != nil {
		fmt.Printf("Error connecting to Couchbase: %v\n", err)
		os.Exit(1)
	}
	defer cluster.Close(nil)

	// // Open the bucket
	// bucket := cluster.Bucket("strfry-data")
	// scope := bucket.Scope("_default")
	// scope.Collection("_default")

	messageFetcher([]string{messageID}, cluster)

	sort.Slice(allUniqueThreadMessages, func(i, j int) bool {
		return allUniqueThreadMessages[i].CreatedAt < allUniqueThreadMessages[j].CreatedAt
	})

	jsonOutput, err := json.MarshalIndent(allUniqueThreadMessages, "", "  ")
	if err != nil {
		fmt.Printf("Error marshaling JSON: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(string(jsonOutput))
}

func messageFetcher(messageIDs []string, cluster *gocb.Cluster) {
	uniqueThreadMessages := []Message{}

	for _, messageID := range messageIDs {
		query := fmt.Sprintf(`
			SELECT d.*
			FROM _default AS d
			USE KEYS "%s"
			UNION
			WITH referencedMessages AS (
				SELECT refMessage.*
				FROM _default AS refMessage
				USE INDEX (kind_and_event_lookup USING GSI)
				WHERE refMessage.kind = 1 AND (ANY t IN refMessage.tags SATISFIES t[0] = "e" AND t[1] = "%s" END)
			)
			SELECT message.content, message.created_at, message.id, message.kind, message.pubkey, message.sig, message.tags
			FROM referencedMessages AS message
		`, messageID, messageID)

		rows, err := cluster.Query(query, nil)
		if err != nil {
			fmt.Printf("Error executing query: %v\n", err)
			continue
		}

		for rows.Next() {
			var message Message
			err := rows.Row(&message)
			if err != nil {
				fmt.Printf("Error scanning row: %v\n", err)
				continue
			}

			if message.Kind == 1 && !containsMessage(allUniqueThreadMessages, message.ID) && !containsMessage(uniqueThreadMessages, message.ID) {
				uniqueThreadMessages = append(uniqueThreadMessages, message)
			}
		}

		rows.Close()
	}

	allUniqueThreadMessages = append(allUniqueThreadMessages, uniqueThreadMessages...)

	messageIDsToQuery := []string{}
	for _, message := range uniqueThreadMessages {
		for _, tag := range message.Tags {
			if len(tag) >= 2 && tag[0] == "e" {
				if !containsMessage(allUniqueThreadMessages, tag[1]) {
					messageIDsToQuery = append(messageIDsToQuery, tag[1])
				}
			}
		}
	}

	if len(messageIDsToQuery) > 0 {
		messageFetcher(messageIDsToQuery, cluster)
	}
}

func containsMessage(messages []Message, messageID string) bool {
	for _, message := range messages {
		if message.ID == messageID {
			return true
		}
	}
	return false
}
