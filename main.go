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
	ParentID  string        `json:"parent_id"`
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

	// Convert messages to MessageView and serialize to JSON
	views := make([]MessageView, 0)
	var flattenMessages func(messages []Message)
	flattenMessages = func(messages []Message) {
		for _, msg := range messages {
			views = append(views, createMessageView(msg))
			flattenMessages(msg.Replies)
		}
	}
	flattenMessages(threadedProcessedMessages)

	// Marshal into JSON
	jsonBytes, err := json.MarshalIndent(views, "", "  ")
	if err != nil {
		log.Fatalf("Error marshalling JSON: %v", err)
	}
	fmt.Println(string(jsonBytes))
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

func processMessageThreading(allUniqueThreadMessages []Message) ([]Message, error) {
	var messagesNestedInAThread []Message

	// Find the original message
	var originalMessage *Message
	var maxMentions int
	for i, msg := range allUniqueThreadMessages {
		etags := getETags(msg.Tags)
		if len(etags) == 0 {
			// Count the number of times the message's ID is mentioned in other messages' etags
			mentions := 0
			for _, otherMsg := range allUniqueThreadMessages {
				if otherMsg.ID != msg.ID {
					for _, etag := range getETags(otherMsg.Tags) {
						if len(etag) > 1 && etag[1] == msg.ID {
							mentions++
						}
					}
				}
			}

			if originalMessage == nil || mentions > maxMentions {
				originalMessage = &allUniqueThreadMessages[i]
				maxMentions = mentions
			} else if mentions == maxMentions {
				return nil, errors.New("multiple original messages found with the same number of mentions")
			}
		}
	}

	if originalMessage == nil {
		return nil, errors.New("original message not found")
	}

	originalMessage.Depth = 1
	messagesNestedInAThread = append(messagesNestedInAThread, *originalMessage)
	allUniqueThreadMessages = removeMessage(allUniqueThreadMessages, originalMessage.ID)

	// Process direct replies to the original message
	for i := 0; i < len(allUniqueThreadMessages); i++ {
		msg := allUniqueThreadMessages[i]
		etags := getETags(msg.Tags)
		if len(etags) == 1 && etags[0][1] == originalMessage.ID {
			msg.Depth = 2
			msg.ParentID = originalMessage.ID
			originalMessage.Replies = append(originalMessage.Replies, msg)
			allUniqueThreadMessages = append(allUniqueThreadMessages[:i], allUniqueThreadMessages[i+1:]...)
			i--
		}
	}

	// Process remaining messages
	for len(allUniqueThreadMessages) > 0 {
		msg := allUniqueThreadMessages[0]
		etags := getETags(msg.Tags)

		var processed bool

		if len(etags) == 1 {
			// Case a: Message has only one etag
			if parentMsg := findMessageByID(messagesNestedInAThread, etags[0][1]); parentMsg != nil {
				msg.Depth = parentMsg.Depth + 1
				msg.ParentID = parentMsg.ID
				parentMsg.Replies = append(parentMsg.Replies, msg)
				processed = true
			}
		} else if len(etags) > 1 {
			// Case b: Message has multiple etags and one of them has etag[3] == "reply"
			for _, etag := range etags {
				if len(etag) >= 4 && etag[3] == "reply" {
					if parentMsg := findMessageByID(messagesNestedInAThread, etag[1]); parentMsg != nil {
						msg.Depth = parentMsg.Depth + 1
						msg.ParentID = parentMsg.ID
						parentMsg.Replies = append(parentMsg.Replies, msg)
						processed = true
						break
					}
				}
			}

			if !processed {
				// Case c: Message has multiple etags and none of them have etag[3] == "reply"
				var maxDepth int
				var parentMsg *Message
				for _, etag := range etags {
					if msg := findMessageByID(messagesNestedInAThread, etag[1]); msg != nil && msg.Depth > maxDepth {
						maxDepth = msg.Depth
						parentMsg = msg
					}
				}
				if parentMsg != nil {
					msg.Depth = parentMsg.Depth + 1
					msg.ParentID = parentMsg.ID
					parentMsg.Replies = append(parentMsg.Replies, msg)
					processed = true
				}
			}
		}

		if processed {
			allUniqueThreadMessages = allUniqueThreadMessages[1:]
		} else {
			// If none of the above cases match, skip the message
			allUniqueThreadMessages = allUniqueThreadMessages[1:]
		}
	}

	return messagesNestedInAThread, nil
}

func removeMessage(messages []Message, id string) []Message {
	for i, msg := range messages {
		if msg.ID == id {
			return append(messages[:i], messages[i+1:]...)
		}
	}
	return messages
}

func getETags(tags []interface{}) [][]string {
	var etags [][]string
	for _, tag := range tags {
		if tagArr, ok := tag.([]interface{}); ok && len(tagArr) > 0 && tagArr[0] == "e" {
			var etag []string
			for _, t := range tagArr {
				if tStr, ok := t.(string); ok {
					etag = append(etag, tStr)
				}
			}
			etags = append(etags, etag)
		}
	}
	return etags
}

func findMessageByID(messages []Message, id string) *Message {
	for i := range messages {
		if messages[i].ID == id {
			return &messages[i]
		}
		if msg := findMessageByID(messages[i].Replies, id); msg != nil {
			return msg
		}
	}
	return nil
}

////////////////////////////////////////////////////////////////////////////////////
// experimenting with formatting output for viewing purposes differently

type MessageView struct {
	ID             string      `json:"id"`
	ParentID       string      `json:"parent_id"`
	CreatedAt      int64       `json:"created_at"`
	User           string      `json:"user"`
	MessageContent interface{} `json:"message_content"`
	Depth          int         `json:"depth"`
}

func createMessageView(msg Message) MessageView {
	return MessageView{
		ID:             msg.ID,
		ParentID:       msg.ParentID,
		CreatedAt:      msg.CreatedAt,
		User:           "@" + msg.Pubkey[:5],
		MessageContent: msg.Content,
		Depth:          msg.Depth,
	}
}
