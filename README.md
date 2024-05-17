# nostr_threads

## Purpose
`nostr_threads` is a service designed to build, manage, and enhance Nostr message threads. It organizes individual Nostr messages into coherent threads, processes media content, and ensures efficient data handling for optimal user experience and search capabilities.

## Goals
1. **Thread Management**:
   - Assemble threads from individual Nostr messages.
   - Maintain a flattened JSON structure for efficient data handling and querying.
2. **Media Handling**:
   - Display media URLs (images, videos, GIFs) inline within threads.
   - Convert non-media URLs into clickable links.
   - Use multimodal models to generate descriptions and metadata for media content.
3. **Relevance and Ranking**:
   - Incorporate metadata to rank search results.
   - Implement fragment highlighting to show users the most relevant parts of the content.

## Features
- **Efficient Thread Assembly**: Organize replies, reactions, and interactions into coherent threads.
- **Media Processing**: Automatically render media inline and generate descriptive metadata.
- **Hybrid Search Support**: Prepare threads for integration with hybrid FTS and vector similarity search.
- **Optimized JSON Structure**: Use a flattened JSON structure to facilitate efficient querying and updating.

## TODO
- [ ] **Thread Management**:
  - [ ] Define and implement a flattened JSON structure for threads.
  - [ ] Develop a mechanism to assemble threads from individual messages.
- [ ] **Media Handling**:
  - [ ] Implement URL parsing to distinguish media from non-media links.
  - [ ] Integrate a multimodal model to generate descriptions and metadata for media URLs.
- [ ] **Relevance and Ranking**:
  - [ ] Develop algorithms to rank threads based on metadata.
  - [ ] Implement fragment highlighting for search results.
- [ ] **Testing**:
  - [ ] Write unit tests for all functionalities.
  - [ ] Perform integration testing with the existing `nostr_site` project.

## Getting Started
### Prerequisites
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

### Usage
1. Start the service:
    ```shell
    go run main.go
    ```

2. Configure `nostr_site` to interact with `nostr_threads`.

### Future Directions

For now, `nostr_threads` is meant as the quickest way to get to a demoable proof of concept. Eventually it might make sense to use a different approach, e.g. modifying and enhancing individual Nostr messages one by one as they come in (importantly adding a "thread_id" to each one). But, there might be tradeoffs that need to be carefully considered, so, TBD.

## Contributing
1. Fork the repository.
2. Create a new branch for your feature/bugfix.
3. Submit a pull request with a detailed description of your changes.

## License
This project is licensed under the MIT License.

---

