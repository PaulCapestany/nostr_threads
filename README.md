# nostr_threads

## Project Purpose
`nostr_threads` is a service meant to focus on handling thread management and media processing for Nostr messages in order for [`nostr_site`](http://github.com/paulcapestany/nostr_site) to be able to offer an optimal user experience and search capabilities.

## Goals

The primary goals of the `nostr_threads` project are:

1. **Thread Management**: Efficiently manage Nostr discussion threads for display via `nostr_site`.
2. **Media Processing**: Identify, classify, and render URLs in Nostr messages as inline media or clickable links.
3. **Multimodal Processing**: Use a multimodal model to create textual descriptions, summaries, and categorization of links to images and other media for use via hybrid Full-Text Search (FTS) and vector similarity search using Couchbase capabilities.

## Key Decisions

1. **Flattened Structure for Threads**:
   - Using a flattened structure approach for dealing with Nostr threads in Couchbase.
2. **Concatenated-thread Embeddings**:
   - Structuring JSON for threads using concatenated-thread embeddings for hybrid FTS and vector similarity search.
3. **Handling URLs**:
   - Identify, classify, and render URLs in Nostr message content as either inline media or clickable links.
4. **Separate Service**:
   - Handling thread management and media processing in a separate service (`nostr_threads`) from the main `nostr_site`.

## Current Progress

1. **Basic Service Setup**:
   - Initial setup of the `nostr_threads` service.
   - Integration with Couchbase for storing and retrieving Nostr messages.
2. **Thread Management**:
   - Implemented basic thread management logic using a flattened structure.
   - Passed initial unit tests for creating and updating threads.
3. **Daemon Service**:
   - Converted `nostr_threads` from a CLI tool to a daemon service that continuously runs.

## Implementation Plan

1. **Define and Implement Flattened JSON Structure for Threads**
   - Redefine the JSON structure and update the Couchbase schema if necessary.

2. **Improve Thread Management Algorithms**
   - Enhance the logic for handling complex threading scenarios.
   - Add unit tests to cover all cases.

3. **Implement URL Parsing and Multimodal Model Integration**
   - Develop the URL parsing logic to handle various edge cases.
   - Integrate the multimodal model to process media URLs and generate metadata.

4. **Integrate with Couchbase Eventing**
   - Set up a Couchbase Eventing function to detect new messages.
   - Define an API endpoint in `nostr_threads` to handle new messages and update threads in real-time.

5. **Enhance Documentation and Testing**
   - Review and update code comments to be godoc compatible.
   - Write comprehensive unit tests and implement regression and integration testing.

## TODO
- [x] **General**
  - [x] Convert `nostr_threads` from a CLI tool to a daemon service that continuously runs.
- [ ] **Thread Management**:
  - [ ] Define and implement a flattened JSON structure for threads.
  - [ ] Improve thread management algorithms to handle complex threading scenarios.
  - [x] Implement unit tests for all thread management functionality.
  - [x] Insert freshly generated threads into Couchbase.
  - [x] Determine the best method for updating threads as new Nostr messages come in.
- [ ] **Media Handling**:
  - [ ] Implement URL parsing to distinguish media from non-media links (tests will be especially important for this).
    - [ ] URL parsing must handle URLs interspersed in text (written by humans), including (but not limited to):
      - [ ] URLs enclosed in quotation marks, parentheses, brackets, etc.
      - [ ] URLs that are missing http://.
      - [ ] URLs that may have commas, periods, etc., directly before/after them.
  - [ ] Integrate a multimodal model to generate textual descriptions and metadata for media URLs.
- [ ] **Documentation and Testing**:
  - [ ] Review existing code comments and, if necessary, provide more detail and/or documentation for all code.
  - [ ] Write unit tests for all functionalities.
  - [ ] Implement regression tests for all features.
  - [ ] Perform integration testing with the existing `nostr_site` project.
- [ ] **Other**:
  - [ ] Set up integrated task/commit/changelog workflow via [semantic-release](https://github.com/go-semantic-release/semantic-release).

## Getting Started

### Prerequisites
- [Go 1.22.2](https://golang.org/dl/)
- [Couchbase Server 7.6](https://www.couchbase.com/downloads)
- [gocb v2.8.1](https://github.com/couchbase/gocb)
- [Nostr protocol specification](https://github.com/nostr-protocol/nips)

### Installation
1. Clone the repository:
    ```shell
    git clone https://github.com/paulcapestany/nostr_threads.git
    cd nostr_threads
    ```
2. Install dependencies:
    ```shell
    go mod tidy
    ```
3. Build/test:
   ```shell
   go mod tidy && go install ./... && go test -v
   ``` 

### Usage
1. Start the service:
    ```shell
    nostr_threads
    ```
2. Configure `nostr_site` to interact with `nostr_threads`.

### Future Directions

For now, `nostr_threads` is meant as the quickest way to get to a demo going for a proof of concept. Eventually it might make sense to use a different approach, e.g. modifying and enhancing individual Nostr messages one by one as they come in (importantly adding a "thread_id" to each one). But, there might be tradeoffs that need to be carefully considered, so, TBD.

## Contributing
1. Fork the repository.
2. Create a new branch for your feature/bugfix.
3. Submit a pull request with a detailed description of your changes.

## Contact

For any questions or suggestions, please contact [Paul](http://github.com/paulcapestany).


---

