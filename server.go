package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/paulcapestany/nostr_shared/config"
	"github.com/paulcapestany/nostr_shared/couchbase"

	"github.com/couchbase/gocb/v2"
	"github.com/gorilla/mux"
)

// JSON structure for our thread document:
// {
// 	"_seen_at_first": 1726153406,
// 	"created_at": 1726153405,
// 	"id": "606ecbb5ce7de4ec4863b62abdd04b6af560d4d929d45a622bd0e579a0259db6",
// 	"kind": 1,
// 	"last_msg_at": 1726153422,
// 	"m_count": 2,
// 	"messages": [
// 	  {
// 		"_seen_at_first": 1726153406,
// 		"content": "first message",
// 		"created_at": 1726153405,
// 		"depth": 1,
// 		"id": "606ecbb5ce7de4ec4863b62abdd04b6af560d4d929d45a622bd0e579a0259db6",
// 		"kind": 1,
// 		"parent_id": "",
// 		"pubkey": "a9868931824e08267706de25cedf822be7ea252d234475732f99e32a1cef64e9",
// 		"sig": "644ff9f6f2ff86a2a406c6ed732f5ec8f80a8b049d324f3cc8048d7ef9557600c8a5e0d72ed1fae3ecf690761d0c2286484e056916f934ba57cbb7ed63cd16b0",
// 		"tags": []
// 	  },
// 	  {
// 		"_seen_at_first": 1726153422,
// 		"content": "second message",
// 		"created_at": 1726153422,
// 		"depth": 2,
// 		"id": "d539e32973efcd3dfd99d31cbd0228c5dba18e364d2502b0d45e96fe2f0413b7",
// 		"kind": 1,
// 		"parent_id": "606ecbb5ce7de4ec4863b62abdd04b6af560d4d929d45a622bd0e579a0259db6",
// 		"pubkey": "a9868931824e08267706de25cedf822be7ea252d234475732f99e32a1cef64e9",
// 		"sig": "57160f8f65439220a0bfef7a191ce4e14492a86999f4215833510cf0535868fd9cefbc8f7d0db81a4ec69979ae1c3aef10b60b8487939b0d1b81c32fae9512dc",
// 		"tags": [
// 		  [
// 			"e",
// 			"606ecbb5ce7de4ec4863b62abdd04b6af560d4d929d45a622bd0e579a0259db6",
// 			"wss://relay.primal.net",
// 			"root"
// 		  ],
// 		  [
// 			"p",
// 			"a9868931824e08267706de25cedf822be7ea252d234475732f99e32a1cef64e9"
// 		  ]
// 		]
// 	  }
// 	],
// 	"pubkey": "a9868931824e08267706de25cedf822be7ea252d234475732f99e32a1cef64e9",
// 	"x_cat_content": "first message  second message  ",
// 	"x_last_processed_at": 1726153422,
// 	"x_last_processed_token_position": 6,
// 	"x_embeddings": {
// 	  "mxbai-embed-large": {
// 		"mxbai-embed-large-embeddings": [
// 		  [0.019370565,-0.019032586],
// 		  [-0.123124121,0.091284109]
// 		]
// 	  },
// 	  "nomic-embed-text": {
// 		"nomic-embed-text-embeddings": [
// 		  [-0.16920707,-0.019894164]
// 		]
// 	  }
// 	}
// }

// nostr_threads flow
// gets an arbitrary message ID, first checks if it's already in the all-nostr-events bucket, if not, uses nak to fetch it

// Message represents a Nostr message structure
type Message struct {
	SeenAtFirst int64         `json:"_seen_at_first"`
	CreatedAt   int64         `json:"created_at"`
	ID          string        `json:"id"`
	Content     interface{}   `json:"content"`
	Kind        int           `json:"kind"`
	Pubkey      string        `json:"pubkey"`
	Sig         string        `json:"sig"`
	Tags        []interface{} `json:"tags"`
	// NOTE: ParentID and Depth fields are not present in the JSON payload, we have to recursively search/construct them
	ParentID     string `json:"parent_id"`
	Depth        int    `json:"depth"`
	XTrustworthy bool   `json:"x_trustworthy"`
}

// Thread represents a flattened Nostr thread structure
type Thread struct {
	// NOTE: a thread's CreatedAt, ID, Kind, and Pubkey are the same as the first message in the thread (the root Nostr message)
	ID          string    `json:"id"`
	CreatedAt   int64     `json:"created_at"`
	LastMsgAt   int64     `json:"last_msg_at"`
	SeenAtFirst int64     `json:"_seen_at_first"`
	MsgCount    int       `json:"m_count"`
	Kind        int       `json:"kind"`
	Pubkey      string    `json:"pubkey"`
	Messages    []Message `json:"messages"`
	// NOTE: fields starting with "x_" may or may not already be present in the JSON payload
	XConcatenatedContent        string                   `json:"x_cat_content"`
	XLastProcessedAt            int64                    `json:"x_last_processed_at"`
	XLastProcessedTokenPosition int                      `json:"x_last_processed_token_position"`
	XEmbeddings                 map[string]EmbeddingInfo `json:"x_embeddings"`
}

// EmbeddingInfo holds the embeddings and token position for a model
type EmbeddingInfo struct {
	Embeddings map[string][][]float32 `json:"-"` // Use map for dynamic field
}

// MarshalJSON will handle the dynamic JSON structure for embeddings
func (ei EmbeddingInfo) MarshalJSON() ([]byte, error) {
	// Create a temporary struct to hold the final JSON

	// Manually construct the embeddings field with the model name as the key
	embeddingsField := make(map[string]interface{})
	for modelName, embeddings := range ei.Embeddings {
		// Generate the key by appending the model name
		key := fmt.Sprintf("%s-embeddings", modelName)
		embeddingsField[key] = embeddings
	}

	// Convert temp struct into a map
	finalJSON := make(map[string]interface{})
	for key, value := range embeddingsField {
		finalJSON[key] = value
	}

	return json.Marshal(finalJSON)
}

func init() {
	// Log to standard output
	log.SetOutput(os.Stdout)
	// Set log flags for more detailed output
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	go func() {
		// This was for pprof profiling
		log.Println(http.ListenAndServe("0.0.0.0:6060", nil))
	}()
}

func main() {
	// Initialize Couchbase connection
	config.Setup()
	cluster, _, _, _, err := couchbase.InitializeCouchbase()
	if err != nil {
		log.Fatalf("Error initializing Couchbase: %v", err)
	}
	defer cluster.Close(nil)

	// Set up the HTTP server
	r := mux.NewRouter()
	r.HandleFunc("/nostr/update_thread", func(w http.ResponseWriter, r *http.Request) {
		UpdateThreadHandler(w, r, cluster)
	}).Methods("POST")

	srv := &http.Server{
		Handler:      r,
		Addr:         "0.0.0.0:8081",
		WriteTimeout: 60 * time.Second,
		ReadTimeout:  60 * time.Second,
		IdleTimeout:  90 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	done := make(chan bool)
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		<-ctx.Done()

		log.Println("Shutting down server...")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		srv.SetKeepAlivesEnabled(false)
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Fatalf("Could not gracefully shut down the server: %v\n", err)
		}
		close(done)
	}()

	log.Println("Starting server on :8081")
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Could not listen on :8081: %v\n", err)
	}

	<-done
	log.Println("Server stopped")
	wg.Wait()
}

func retryOperation(operation func() error, retries int, messageIDsToQuery []string) error {
	for i := 0; i < retries; i++ {
		err := operation()
		if err == nil {
			return nil
		}
		log.Printf("Retry %d/%d failed: %v messageIDsToQuery:%v", i+1, retries, err, messageIDsToQuery)
		time.Sleep(2 * time.Second) // Exponential backoff can be implemented here
	}
	return fmt.Errorf("operation failed after %d retries", retries)
}

const trustOlderTimestamps int64 = 1725897900 // Sep 9, 2024 4:05 PM as Unix timestamp

// isMessageTimestampTrustworthy checks if a message's created_at or _seen_at_first is trustworthy for appending to x_cat_content.
// Parent-child validation is always enforced, ensuring a child cannot precede its parent in terms of timestamps.
func isMessageTimestampTrustworthy(messageCreatedAt int64, lastMsgAt int64, seenAtFirst *int64, parentCreatedAt *int64, parentSeenAtFirst *int64) bool {
	// Parent-child validation: A child message cannot precede its parent in either created_at or _seen_at_first.
	if parentCreatedAt != nil && messageCreatedAt < *parentCreatedAt {
		log.Printf("Message created_at %d is earlier than its parent's created_at %d; rejecting message.", messageCreatedAt, *parentCreatedAt)
		return false
	}

	if parentSeenAtFirst != nil && seenAtFirst != nil && *seenAtFirst < *parentSeenAtFirst {
		log.Printf("Message _seen_at_first %d is earlier than its parent's _seen_at_first %d; rejecting message.", *seenAtFirst, *parentSeenAtFirst)
		return false
	}

	// Trust messages with a _seen_at_first value
	if seenAtFirst != nil && *seenAtFirst != 0 {
		return true
	}

	// Trust backfilled messages older than the cutoff timestamp
	if messageCreatedAt < trustOlderTimestamps {
		return true
	}

	// If no _seen_at_first, but created_at is newer than last_msg_at, trust the created_at.
	if messageCreatedAt > lastMsgAt {
		return true
	}

	// Otherwise, the timestamp is not trustworthy
	return false
}

// SanitizeContent remains unchanged
func SanitizeContent(content string) string {
	return strings.Join(strings.Fields(content), " ") // Reduce multiple spaces to a single space
}

// UpdateThreadHandler handles requests to update threads and ensure the x_cat_content is append-only
func UpdateThreadHandler(w http.ResponseWriter, r *http.Request, cluster *gocb.Cluster) {
	// Decode payload
	var payload struct {
		ID      string  `json:"id"`
		Message Message `json:"message"`
	}
	err := json.NewDecoder(r.Body).Decode(&payload)
	if err != nil {
		log.Printf("Failed to decode payload: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	log.Printf("Received payload with ID: %+v\n", payload.ID)

	// Couchbase setup
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	bucket := cluster.Bucket(config.EnvPrefix + "threads")
	collection := bucket.DefaultCollection()

	var existingThread Thread
	var cas gocb.Cas // Store the CAS value
	// Fetch existing thread
	getResult, err := collection.Get(payload.ID, nil)
	if err == nil {
		cas = getResult.Cas() // Retrieve the CAS value
		err = getResult.Content(&existingThread)
		if err != nil {
			log.Printf("Failed to decode existing thread content: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	} else if errors.Is(err, gocb.ErrDocumentNotFound) {
		// Document does not exist; initialize CAS to 0
		cas = 0
		existingThread = Thread{} // Empty thread
	} else {
		// Handle other errors
		log.Printf("Failed to get existing thread: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Process messages and fetch new ones
	messageIDsToQuery := []string{payload.ID}
	log.Println("Calling messageFetcher with messageIDsToQuery: ", messageIDsToQuery)
	var allUniqueThreadMessages []Message
	alreadyQueriedIDs := make(map[string]bool)
	foundMessageIDs := make(map[string]bool)
	err = retryOperation(func() error {
		return messageFetcher(ctx, messageIDsToQuery, &allUniqueThreadMessages, cluster, alreadyQueriedIDs, foundMessageIDs)
	}, 3, messageIDsToQuery)

	if err != nil {
		log.Printf("Failed to fetch messages: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Sort messages by created_at
	sort.Slice(allUniqueThreadMessages, func(i, j int) bool {
		return allUniqueThreadMessages[i].CreatedAt < allUniqueThreadMessages[j].CreatedAt
	})

	// Process threading logic and apply parent-child validation
	threadedProcessedMessages, err := processMessageThreading(allUniqueThreadMessages)
	if err != nil {
		log.Printf("Failed to process message threading: %v messageIDsToQuery: %v", err, messageIDsToQuery)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Generate new x_cat_content based on append-only rules
	var allMessagesContent string
	for i, msg := range threadedProcessedMessages {
		sanitizedContent := SanitizeContent(fmt.Sprintf("%s", msg.Content))

		// Fetch the parent message (if it exists)
		var parentCreatedAt, parentSeenAtFirst *int64
		if msg.ParentID != "" {
			for _, parentMsg := range threadedProcessedMessages {
				if parentMsg.ID == msg.ParentID {
					parentCreatedAt = &parentMsg.CreatedAt
					parentSeenAtFirst = &parentMsg.SeenAtFirst
					break
				}
			}
		}

		// Trust the message if it passes the universal trust checks and update the trust flag
		msg.XTrustworthy = isMessageTimestampTrustworthy(msg.CreatedAt, existingThread.LastMsgAt, &msg.SeenAtFirst, parentCreatedAt, parentSeenAtFirst)

		// Append message content to x_cat_content regardless of trustworthiness
		allMessagesContent += fmt.Sprintf("%s  ", sanitizedContent)

		// Save the updated message back into the slice
		threadedProcessedMessages[i] = msg
	}

	// Update last_msg_at based on the latest trustworthy message
	var lastMsgAt int64
	for _, msg := range threadedProcessedMessages {
		if msg.CreatedAt > lastMsgAt {
			lastMsgAt = msg.CreatedAt
		}
	}
	mCount := len(threadedProcessedMessages)

	// Construct the updated thread
	newThread := Thread{
		CreatedAt:            threadedProcessedMessages[0].CreatedAt,
		SeenAtFirst:          threadedProcessedMessages[0].SeenAtFirst,
		LastMsgAt:            lastMsgAt,
		MsgCount:             mCount,
		ID:                   threadedProcessedMessages[0].ID,
		Kind:                 threadedProcessedMessages[0].Kind,
		Pubkey:               threadedProcessedMessages[0].Pubkey,
		Messages:             threadedProcessedMessages,
		XConcatenatedContent: allMessagesContent,
	}

	// Preserve existing fields
	newThread.XLastProcessedAt = existingThread.XLastProcessedAt
	newThread.XLastProcessedTokenPosition = existingThread.XLastProcessedTokenPosition
	newThread.XEmbeddings = existingThread.XEmbeddings

	// Implement CAS-based optimistic concurrency control
	const maxRetries = 5
	retries := 0
	for {
		var mutationErr error
		if cas == 0 {
			// Document does not exist, try to insert
			_, mutationErr = collection.Insert(newThread.ID, newThread, nil)
			if mutationErr != nil {
				if errors.Is(mutationErr, gocb.ErrDocumentExists) {
					// Another process created the document after we checked
					cas = 0
					// Fetch the document to get the CAS and existing content
					getResult, err := collection.Get(newThread.ID, nil)
					if err != nil {
						log.Printf("Failed to fetch existing thread after insert conflict: %v", err)
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					cas = getResult.Cas()
					err = getResult.Content(&existingThread)
					if err != nil {
						log.Printf("Failed to decode existing thread content after insert conflict: %v", err)
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					// Merge threads
					mergedThread, err := mergeThreads(existingThread, newThread)
					if err != nil {
						log.Printf("Failed to merge threads: %v", err)
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					newThread = mergedThread
					continue // Retry with Replace operation
				} else {
					log.Printf("Failed to insert thread: %v", mutationErr)
					http.Error(w, mutationErr.Error(), http.StatusInternalServerError)
					return
				}
			} else {
				// Insert succeeded
				log.Printf("Final saved thread (insert): ID=%s, MsgCount=%d", newThread.ID, newThread.MsgCount)
				break
			}
		} else {
			// Document exists, try to replace with CAS
			replaceOptions := &gocb.ReplaceOptions{
				Cas: cas,
			}
			_, mutationErr = collection.Replace(newThread.ID, newThread, replaceOptions)
			if mutationErr != nil {
				if errors.Is(mutationErr, gocb.ErrCasMismatch) {
					// CAS mismatch occurred
					if retries >= maxRetries {
						log.Printf("Max retries reached for CAS mismatch: %v", mutationErr)
						http.Error(w, "Failed to update thread due to concurrent modifications", http.StatusInternalServerError)
						return
					}

					retries++
					log.Printf("CAS mismatch detected. Retry %d/%d", retries, maxRetries)

					// Re-fetch the latest document and its CAS
					getResult, err := collection.Get(newThread.ID, nil)
					if err != nil {
						if errors.Is(err, gocb.ErrDocumentNotFound) {
							// Document was deleted, try to insert
							cas = 0
							existingThread = Thread{} // Empty thread
						} else {
							log.Printf("Failed to re-fetch thread during CAS mismatch handling: %v", err)
							http.Error(w, err.Error(), http.StatusInternalServerError)
							return
						}
					} else {
						cas = getResult.Cas()
						err = getResult.Content(&existingThread)
						if err != nil {
							log.Printf("Failed to decode existing thread content during CAS mismatch handling: %v", err)
							http.Error(w, err.Error(), http.StatusInternalServerError)
							return
						}
					}

					// Merge newThread with existingThread
					mergedThread, err := mergeThreads(existingThread, newThread)
					if err != nil {
						log.Printf("Failed to merge threads: %v", err)
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					newThread = mergedThread
					// Continue to retry with updated CAS
					continue
				} else if errors.Is(mutationErr, gocb.ErrDocumentNotFound) {
					// Document was deleted, try to insert
					cas = 0
					existingThread = Thread{} // Empty thread
					continue
				} else {
					log.Printf("Failed to replace thread: %v", mutationErr)
					http.Error(w, mutationErr.Error(), http.StatusInternalServerError)
					return
				}
			} else {
				// Replace succeeded
				log.Printf("Final saved thread (replace): ID=%s, MsgCount=%d", newThread.ID, newThread.MsgCount)
				break
			}
		}
	}

	// Respond with the updated thread
	responseJSON, err := json.Marshal(newThread)
	if err != nil {
		log.Printf("Failed to marshal response JSON: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
	w.Write(responseJSON)
}

// mergeThreads merges the existing thread with the new thread, ensuring x_cat_content is append-only
func mergeThreads(existingThread, newThread Thread) (Thread, error) {
	// Merge messages, avoiding duplicates
	messageMap := make(map[string]Message)

	// Add all messages from existingThread to the map first
	for _, msg := range existingThread.Messages {
		messageMap[msg.ID] = msg
	}

	// Iterate over newThread messages and update/merge them
	for _, newMsg := range newThread.Messages {
		if existingMsg, exists := messageMap[newMsg.ID]; exists {
			// Merge logic: update XTrustworthy flag
			existingMsg.XTrustworthy = existingMsg.XTrustworthy || newMsg.XTrustworthy
			messageMap[newMsg.ID] = existingMsg // Update the message in the map
		} else {
			messageMap[newMsg.ID] = newMsg
		}
	}

	// Sort messages by created_at
	mergedMessages := make([]Message, 0, len(messageMap))
	for _, msg := range messageMap {
		mergedMessages = append(mergedMessages, msg)
	}

	sort.Slice(mergedMessages, func(i, j int) bool {
		return mergedMessages[i].CreatedAt < mergedMessages[j].CreatedAt
	})

	// Rebuild x_cat_content, ensuring we only append content for new messages
	var allMessagesContent string
	for _, msg := range mergedMessages {
		sanitizedContent := SanitizeContent(fmt.Sprintf("%s", msg.Content))
		allMessagesContent += fmt.Sprintf("%s  ", sanitizedContent)
	}

	var lastMsgAt int64
	for _, msg := range mergedMessages {
		if msg.CreatedAt > lastMsgAt {
			lastMsgAt = msg.CreatedAt
		}
	}
	mCount := len(mergedMessages)

	// Construct the merged thread
	mergedThread := Thread{
		CreatedAt:                   existingThread.CreatedAt,
		SeenAtFirst:                 existingThread.SeenAtFirst,
		LastMsgAt:                   lastMsgAt,
		MsgCount:                    mCount,
		ID:                          existingThread.ID,
		Kind:                        existingThread.Kind,
		Pubkey:                      existingThread.Pubkey,
		Messages:                    mergedMessages,
		XConcatenatedContent:        allMessagesContent,
		XLastProcessedAt:            existingThread.XLastProcessedAt,
		XLastProcessedTokenPosition: existingThread.XLastProcessedTokenPosition,
		XEmbeddings:                 existingThread.XEmbeddings,
	}

	return mergedThread, nil
}

func messageFetcher(ctx context.Context, messageIDs []string, allUniqueThreadMessages *[]Message, cluster *gocb.Cluster, alreadyQueriedIDs map[string]bool, foundMessageIDs map[string]bool) error {
	if cluster == nil {
		log.Println("Cluster connection is not initialized.")
		return fmt.Errorf("cluster connection is not initialized")
	}

	messageIDsToQuery := make([]string, 0)
	messageMap := make(map[string]Message)
	for _, msg := range *allUniqueThreadMessages {
		messageMap[msg.ID] = msg
	}

	for _, id := range messageIDs {
		if alreadyQueriedIDs[id] {
			continue // Skip IDs already marked as missing
		}

		query := fmt.Sprintf(`WITH referencedMessages AS (
            SELECT d.*
            FROM `+"`all-nostr-events`._default._default"+` AS d
            USE KEYS "%s"

            UNION

            SELECT refMessage.*
            FROM `+"`all-nostr-events`._default._default"+` AS refMessage
            USE INDEX (kind_and_event_lookup USING GSI)
            WHERE refMessage.kind = 1 AND (ANY t IN refMessage.tags SATISFIES t[0] = "e" AND t[1] = "%s" END)
        )
        SELECT message.content, message.created_at, message._seen_at_first, message.id, message.kind, message.pubkey, message.sig, message.tags
        FROM referencedMessages AS message`, id, id)

		results, err := cluster.Query(query, nil)
		if err != nil {
			log.Printf("Failed to execute query for ID %s: %v", id, err)
			continue
		} else {
			alreadyQueriedIDs[id] = true
		}

		for results.Next() {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			var msg Message
			if err := results.Row(&msg); err != nil {
				log.Printf("Failed to parse message: %v", err)
				continue
			}
			if msg.Kind != 1 {
				continue
			}

			contentStr := fmt.Sprintf("%v", msg.Content)
			msg.Content = contentStr

			for _, tag := range msg.Tags {
				tagSlice, ok := tag.([]interface{})
				if !ok || len(tagSlice) < 2 || tagSlice[0] != "e" {
					continue
				}
				if idStr, ok := tagSlice[1].(string); ok && !contains(foundMessageIDs, idStr) && !contains(alreadyQueriedIDs, idStr) {
					messageIDsToQuery = append(messageIDsToQuery, idStr)
				}
			}
			if !containsMessage(foundMessageIDs, msg.ID) {
				messageIDsToQuery = append(messageIDsToQuery, msg.ID)
				*allUniqueThreadMessages = append(*allUniqueThreadMessages, msg)
				foundMessageIDs[msg.ID] = true
			}
		}
		if err := results.Err(); err != nil {
			log.Printf("Error iterating results: %v", err)
		}
	}

	// Recursively fetch messages for newly discovered IDs if there are any
	if len(messageIDsToQuery) > 0 {
		return messageFetcher(ctx, messageIDsToQuery, allUniqueThreadMessages, cluster, alreadyQueriedIDs, foundMessageIDs)
	}
	// log.Printf("alreadyQueriedIDs: %v", alreadyQueriedIDs)
	log.Printf("foundMessageIDs: %v", foundMessageIDs)
	return nil
}

func containsMessage(messages map[string]bool, id string) bool {
	_, exists := messages[id]
	return exists
}

func contains(ids map[string]bool, id string) bool {
	_, exists := ids[id]
	return exists
}

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
				// log.Printf("Warning: potential error/fail? (mentions > maxMentions) \"originalMessage.ID\": %v", originalMessage.ID)
			} else if mentions == maxMentions {
				return nil, errors.New("multiple original messages found with the same number of mentions")
			}
		}
	}

	if originalMessage == nil {
		// log.Printf("original message not found: %v", originalMessage.ID)
		return nil, errors.New("WARN: original message not found")
		// TODO: grab message from another relay?
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
			messagesNestedInAThread = append(messagesNestedInAThread, msg)
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
				messagesNestedInAThread = append(messagesNestedInAThread, msg)
				processed = true
			}
		} else if len(etags) > 1 {
			// Case b: Message has multiple etags and one of them has etag[3] == "reply"
			for _, etag := range etags {
				if len(etag) >= 4 && etag[3] == "reply" {
					if parentMsg := findMessageByID(messagesNestedInAThread, etag[1]); parentMsg != nil {
						msg.Depth = parentMsg.Depth + 1
						msg.ParentID = parentMsg.ID
						messagesNestedInAThread = append(messagesNestedInAThread, msg)
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
					messagesNestedInAThread = append(messagesNestedInAThread, msg)
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
	}
	return nil
}
