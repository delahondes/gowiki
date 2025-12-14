# GoWiki - A Modern, Simple Wiki in Go

## Design Philosophy

GoWiki aims to combine the best aspects of traditional wikis like Dokuwiki with modern development practices, using Go for performance and simplicity.

## Core Design Choices

### 1. Markdown as Primary Format

**Rationale:**
- Markdown is the de facto standard for lightweight markup
- Simpler than Dokuwiki's syntax
- Widely supported with excellent tooling
- Easy to read and write in plain text

**Implementation:**
- Use `goldmark` or `blackfriday` for markdown parsing
- Support CommonMark specification
- Extensible for custom markdown extensions via plugins

### 2. Attic-Style Version Control

**Rationale:**
- Simple and transparent file-based storage
- 1:1 mapping between wiki paths and filesystem (like Dokuwiki)
- Easy to understand and maintain
- No complex database or git dependencies
- Human-readable revisions

**Implementation:**

```
wiki/
  pages/                  # Current versions
    start.txt             # Current version
    about/
      index.txt
    projects/
      myproject.txt

  attic/                 # All revisions
    start/
      1672531200.txt     # Revision 1 (timestamp)
      1672617600.txt     # Revision 2
    about/
      index/
        1672531200.txt
        1672617600.txt
```

**Features:**
- Timestamp-based revision filenames
- Atomic operations (write revision before updating current)
- Metadata stored in separate JSON files
- Easy diff and restore operations

### 3. Plugin Architecture

**Rationale:**
- Keep core minimal and focused
- Easy extensibility (like Dokuwiki's plugin system)
- Community contributions
- Customization without core modifications
- **Managed lifecycle** with compile/deploy cycle

**Key Features:**
- **Compile-time integration**: Plugins are compiled into the main binary
- **Managed lifecycle**: Automatic compile/deploy when adding plugins
- **Rollback capability**: Easy revert if plugin causes issues
- **Isolation**: Each plugin has its own namespace and dependencies

**Implementation:**

```go
// Plugin interface
type Plugin interface {
    Init(config map[string]interface{}) error
    Name() string
    Version() string
    Description() string
    
    // Lifecycle hooks
    OnStart() error
    OnStop() error
    OnUpgrade(fromVersion string) error
    OnDowngrade(toVersion string) error
    
    // Extension points
    RegisterRoutes(router *Router)
    RegisterMarkdownExtensions(md goldmark.Markdown)
    OnPageRender(ctx *PageContext) error
    OnPageSave(ctx *PageContext) error
}
```

**Plugin Lifecycle:**
1. **Install**: `gowiki plugin install github.com/user/plugin`
2. **Compile**: System automatically rebuilds with new plugin
3. **Deploy**: Atomic replacement of running binary
4. **Verify**: Health checks ensure plugin works correctly
5. **Rollback**: Automatic revert if issues detected

**Plugin Types:**
- **Renderer plugins**: Custom markdown extensions
- **Storage plugins**: Alternative backends (SQL, Git, etc.)
- **Auth plugins**: Different authentication methods
- **UI plugins**: Themes and components
- **API plugins**: Additional endpoints
- **Event plugins**: Webhooks and notifications

**Advantages:**
- Single binary deployment maintains simplicity
- Automatic dependency management
- Version compatibility checking
- Sandboxed plugin execution
- Easy distribution via plugin registry

### 4. Dual Editor System

**Rationale:**
- Provide both simple and advanced editing options
- Cater to different user preferences
- Progressive enhancement approach

**Implementation:**
- **Basic Editor**: Simple textarea with live preview
- **Advanced Editor**: Optional WYSIWYG (Tiptap/Monaco)
- Server-side markdown rendering for preview
- Client-side JavaScript for enhanced experience

## Architecture Principles

### Engine vs Data Separation

**Core Principle**: Complete separation between the wiki engine and data storage.

```
Engine (gowiki binary)
    ↓ (uses interface)
Storage Implementation
    ↓ (reads/writes)
Data Files (wiki content)
```

**Benefits:**
- Engine can be updated independently of data
- Multiple storage backends (file, git, SQL, etc.)
- Easy migration between storage systems
- Clean API boundaries
- Better testability

**Implementation:**

```go
// Storage interface (engine/storage/interface.go)
type PageStorage interface {
    GetPage(path string) (*Page, error)
    SavePage(path, content string, meta Metadata) error
    GetRevisions(path string) ([]Revision, error)
    // ... other storage operations
}

// Multiple implementations:
// - data/wiki/fs.go (file system - default)
// - data/wiki/git.go (git backend)
// - data/wiki/sql.go (SQL database)
```

## Technical Stack

### Backend
- **Language**: Go (for performance and simplicity)
- **Web Framework**: [Chi](https://github.com/go-chi/chi) - lightweight router with excellent wildcard support
- **Markdown**: [Goldmark](https://github.com/yuin/goldmark) - CommonMark compliant with excellent extensibility for plugins
- **Storage**: File system (attic-style versioning) as default implementation
- **Configuration**: JSON or YAML files
- **HTTP Server**: Standard library `net/http`

### Frontend
- **Base**: HTML templates (Go's `html/template`)
- **Enhancement**: Optional JavaScript for interactive features
- **Editor**: Dual system (markdown textarea + live preview)
- **Styling**: Minimal CSS (easy to theme)
- **Routing**: Query-parameter based URLs (like Dokuwiki) using `?action=edit`, `?action=view`, etc.

### Key Library Details

**Chi Router:**
- Chosen for its lightweight design and excellent routing capabilities
- Supports wildcard paths (`/{pagePath:*}`) perfect for wiki URLs
- Middleware support for authentication, logging, etc.
- Full compatibility with standard library

**Goldmark Markdown:**
- Full CommonMark compliance as base specification
- AST-based architecture enables deep extensibility
- Multiple extension points: parser hooks, renderer hooks, AST transformers
- Plugin-friendly design allows custom syntax extensions
- Good performance with extensible architecture
- Supports GitHub Flavored Markdown extensions

## Project Structure

```
gowiki/
  engine/          # Core wiki engine (pure logic, no data)
    wiki/         # Wiki processing logic
    plugins/      # Plugin management system
    storage/      # Storage interfaces
    auth/         # Authentication interfaces
    
  data/           # Data storage implementations
    wiki/         # File system storage (default)
    git/          # Git storage backend (optional)
    sql/          # SQL storage backend (optional)
    
  cmd/            # Command-line applications
    gowiki/       # Main web server
    gowiki-cli/   # CLI administration tool
    
  web/            # Web interface
    server/       # HTTP server implementation
    templates/    # HTML templates
    static/       # CSS/JS assets
    
  plugins/        # Built-in plugins (compiled in)
    auth/         # Authentication plugins
    render/       # Rendering plugins
    storage/      # Storage plugins
    
  wiki/           # Default wiki content location
    pages/        # Current page versions
    attic/        # Revision history
    meta/         # Metadata files
    
  config/         # Configuration files
    wiki.json     # Main configuration
    plugins/      # Plugin configurations
```

**Key Separation:**
- `engine/` contains only interfaces and pure logic
- `data/` contains concrete storage implementations
- Plugins can implement either engine interfaces or data interfaces
- Web interface depends only on engine interfaces

## URL Design

GoWiki uses query-parameter based URLs following the Dokuwiki convention:

- **View page**: `/page-name` or `/page-name?action=view`
- **Edit page**: `/page-name?action=edit`
- **Save page**: `POST /page-name?action=save`
- **Create new page**: `/new-page?action=edit`

**Rationale:**
- Clear separation between page paths and actions
- Avoids ambiguity where `/page/edit` could be interpreted as a subpage
- Consistent with established wiki systems
- Easy to extend with additional actions

## Key Features

### Already Decided
- ✅ Markdown support
- ✅ Attic-style version control
- ✅ Plugin architecture with managed lifecycle
- ✅ Dual editor system
- ✅ File-based storage
- ✅ Engine/data separation

### To Be Designed
- ❓ Authentication system
- ❓ Search functionality
- ❓ Access control
- ❓ API design
- ❓ Plugin discovery and registry
- ❓ Automatic compile/deploy cycle for plugins
- ❓ Health checks and rollback mechanism

## Getting Started

### Prerequisites
- Go 1.18+
- Basic understanding of Markdown

### Installation
```bash
# Clone the repository
git clone https://github.com/delahondes/gowiki.git
cd gowiki

# Build
go build -o gowiki ./cmd/gowiki

# Run
./gowiki
```

### Configuration
Edit `config/wiki.json` to customize:
- Port and binding
- Wiki title and description
- Plugin directories
- Authentication settings

## Development Roadmap

Our development follows a phased approach, building from core functionality to advanced features:

### Phase 1: Core Wiki Engine (Current Focus)
**Goal**: Basic wiki functionality without plugins

- [x] Page storage and retrieval system
- [ ] Attic-style version control implementation
- [x] Basic markdown rendering with Goldmark
- [x] Simple web interface (view/edit pages)
- [x] Query-parameter based routing with Chi
- [x] File system storage backend
- [ ] Basic configuration system

**Deliverable**: Functional wiki with manual page creation/editing

### Phase 2: Plugin Infrastructure
**Goal**: Extensible architecture for customization

- [ ] Plugin interface definition and registration
- [ ] Compile-time plugin integration system
- [ ] Plugin lifecycle management (init, start, stop)
- [ ] Extension points for core functionality
- [ ] Example plugins (basic auth, custom syntax)
- [ ] Plugin configuration system
- [ ] Health checks and rollback mechanism

**Deliverable**: Wiki with plugin support and basic extensions

### Phase 3: Enhanced Editing Experience
**Goal**: Improved user interface and editing workflow

- [ ] **SimpleMDE integration** (lightweight Markdown editor)
- [ ] Live preview with Goldmark rendering
- [ ] Dual editor system (simple + enhanced)
- [ ] Editor toolbar with common Markdown actions
- [ ] Image/upload support
- [ ] Table editing helpers
- [ ] Mobile-responsive interface

**Editor Choice Rationale:**
- SimpleMDE selected over ProseMirror-based editors
- Lightweight and easy to integrate
- Good balance of features without complexity
- Works well with server-side Goldmark rendering
- No complex plugin system required

### Phase 4: Advanced Features
**Goal**: Production-ready wiki system

- [ ] Authentication system (multiple backends)
- [ ] Access control and permissions
- [ ] Full-text search
- [ ] REST API endpoints
- [ ] Webhook notifications
- [ ] Export/import functionality
- [ ] Advanced plugin examples

### Phase 5: Polish and Optimization
**Goal**: Production deployment ready

- [ ] Performance optimization
- [ ] Security hardening
- [ ] Documentation
- [ ] Deployment scripts
- [ ] Monitoring and metrics
- [ ] Backup/restore system
- [ ] Migration tools

## Implementation Priority

```
Core Engine → Plugin System → Enhanced Editor → Advanced Features → Production Ready
```

**Rationale:**
1. Core engine provides immediate functionality
2. Plugin system enables extensibility
3. Enhanced editor improves user experience
4. Advanced features add production capabilities
5. Polish ensures reliability and maintainability

## Editor Evolution Path

1. **Phase 1**: Basic textarea + preview
2. **Phase 3**: SimpleMDE integration
3. **Future**: Optional WYSIWYG (Tiptap/Monaco) as plugin

This allows us to start simple and add complexity only when needed.

## Contributing

We welcome contributions! Please see `CONTRIBUTING.md` for guidelines.

## License

This project is licensed under the MIT License - see the `LICENSE` file for details.

## Inspiration

GoWiki is inspired by:
- Dokuwiki (simplicity and plugin system)
- Modern static site generators
- Git's version control concepts (simplified)
- CommonMark specification

## Alternatives Considered

- **Git-based versioning**: Too complex for primary use
- **Database storage**: Adds unnecessary complexity
- **Single-file storage**: Less transparent than attic system
- **PHP implementation**: Go provides better performance and maintainability
