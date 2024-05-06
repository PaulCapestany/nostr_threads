package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"

	"github.com/couchbase/gocb/v2"
)

type Message struct {
	Content   interface{}   `json:"content"`
	CreatedAt int64         `json:"created_at"`
	ID        string        `json:"id"`
	Kind      int           `json:"kind"`
	Pubkey    string        `json:"pubkey"`
	Sig       string        `json:"sig"`
	Tags      []interface{} `json:"tags"`
	Depth     int           `json:"depth"`
	Replies   []Message     `json:"replies"`
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

	threadedProcessedMessages, err := processMessageThreading(allUniqueThreadMessages)
	if err != nil {
		log.Fatalf("Error with processMessageThreading: %v", err)
	}

	output, err := json.Marshal(threadedProcessedMessages)
	if err != nil {
		log.Fatalf("Failed to marshal messages: %v", err)
	}
	fmt.Println(string(output))
}

func messageFetcher(messageIDs []string) {
	if cluster == nil {
		log.Println("Cluster connection is not initialized.")
		return
	}

	var messageIDsToQuery []string // To collect all new message IDs from tags for further queries

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

		for results.Next() {
			var msg Message
			if err := results.Row(&msg); err != nil {
				log.Printf("Failed to parse message: %v", err)
				continue
			}
			if msg.Kind != 1 {
				continue // Skip messages that are not of kind 1
			}

			// Convert content to string regardless of its original type
			contentStr := fmt.Sprintf("%v", msg.Content)
			msg.Content = contentStr

			// Process tags to find new message IDs to query
			for _, tag := range msg.Tags {
				tagSlice, ok := tag.([]interface{})
				if !ok || len(tagSlice) < 2 || tagSlice[0] != "e" {
					continue
				}
				if idStr, ok := tagSlice[1].(string); ok && !containsMessage(allUniqueThreadMessages, idStr) && !contains(messageIDsToQuery, idStr) {
					messageIDsToQuery = append(messageIDsToQuery, idStr)
				}
			}
			if !containsMessage(allUniqueThreadMessages, msg.ID) {
				messageIDsToQuery = append(messageIDsToQuery, msg.ID)
				allUniqueThreadMessages = append(allUniqueThreadMessages, msg) // Add to global slice if not already present
			}
		}
		if err := results.Err(); err != nil {
			log.Printf("Error iterating results: %v", err)
		}
	}

	// Recursively fetch messages for newly discovered IDs if there are any
	if len(messageIDsToQuery) > 0 {
		messageFetcher(messageIDsToQuery)
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

func contains(ids []string, id string) bool {
	for _, existingID := range ids {
		if existingID == id {
			return true
		}
	}
	return false
}

// -------------------------------------------------------//
// The following deals with nesting/threading of messages //
// -------------------------------------------------------//

// Helper function to convert any tag to a string, and filter e-tags
func filterETags(tags []interface{}) []string {
	var eTags []string
	for _, tag := range tags {
		tagSlice, ok := tag.([]interface{})
		if !ok || len(tagSlice) < 2 {
			continue // Skip if it's not a slice or too short
		}
		tagType, ok := tagSlice[0].(string)
		if !ok || tagType != "e" {
			continue // Skip if the first element is not string "e"
		}
		etagID := fmt.Sprintf("%v", tagSlice[1]) // Convert second element to string, regardless of its original type
		eTags = append(eTags, etagID)
	}
	return eTags
}

// Find the original message (depth 1)
func findOriginalMessage(messages []Message) (Message, error) {
	for _, msg := range messages {
		eTags := filterETags(msg.Tags)
		if len(eTags) == 0 {
			msg.Depth = 1
			return msg, nil
		}
	}
	return Message{}, errors.New("no original message found")
}

// Recursive function to assign replies to the right parent and return remaining messages
func assignReplies(parent Message, allMessages []Message) (Message, []Message) {
	var remainingMessages []Message
	parent.Replies = []Message{} // Ensure replies are initialized

	for _, msg := range allMessages {
		eTags := filterETags(msg.Tags)
		if len(eTags) == 1 && eTags[0] == parent.ID {
			msg.Depth = parent.Depth + 1
			var updatedMsg Message
			updatedMsg, allMessages = assignReplies(msg, allMessages) // recursively assign deeper replies
			parent.Replies = append(parent.Replies, updatedMsg)
		} else {
			remainingMessages = append(remainingMessages, msg)
		}
	}
	return parent, remainingMessages
}

func processMessageThreading(allMessages []Message) ([]Message, error) {
	originalMessage, err := findOriginalMessage(allMessages)
	if err != nil {
		return nil, err
	}
	allMessages = append(allMessages[:0], allMessages[1:]...) // remove the original message from slice

	nestedThread, remaining := assignReplies(originalMessage, allMessages)
	if len(remaining) > 0 {
		log.Printf("Remaining messages not processed: %+v\n", remaining)
	}

	return []Message{nestedThread}, nil
}
