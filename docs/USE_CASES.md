# Conduit Real-World Use Cases

**Version**: 0.1.0
**Last Updated**: December 2025

---

## Overview

This document provides practical, real-world examples of how to use Conduit to enhance your AI-assisted development workflow. Each use case includes step-by-step instructions, configuration examples, and tips for success.

## Prerequisites

Before following these use cases, ensure Conduit is installed and running:

```bash
# Install Conduit (if not already installed)
curl -fsSL https://raw.githubusercontent.com/amlandas/Conduit-AI-Intelligence-Hub/main/scripts/install.sh | bash

# Verify installation
conduit status
conduit doctor
```

---

## Table of Contents

1. [Use Case 1: Project Documentation Search](#use-case-1-project-documentation-search)
2. [Use Case 2: Multi-Repository Code Search](#use-case-2-multi-repository-code-search)
3. [Use Case 3: Database Query Assistant](#use-case-3-database-query-assistant)
4. [Use Case 4: API Testing & Documentation](#use-case-4-api-testing--documentation)
5. [Use Case 5: Log Analysis Helper](#use-case-5-log-analysis-helper)
6. [Use Case 6: Cloud Infrastructure Context](#use-case-6-cloud-infrastructure-context)
7. [Use Case 7: Research Paper Knowledge Base](#use-case-7-research-paper-knowledge-base)
8. [Use Case 8: Team Wiki Integration](#use-case-8-team-wiki-integration)

---

## Use Case 1: Project Documentation Search

### Scenario
You're working on a large project with extensive documentation spread across multiple directories. You want Claude Code to be able to search and reference this documentation when answering questions.

### Setup

```bash
# Add your documentation directories
conduit kb add ~/projects/myapp/docs --name "MyApp Docs"
conduit kb add ~/projects/myapp/api-specs --name "API Specifications"
conduit kb add ~/projects/myapp/architecture --name "Architecture Docs"

# Sync all sources
conduit kb sync

# Verify indexing
conduit kb stats
```

### Usage with Claude Code

After binding the KB to Claude Code, you can ask:

> "Based on our project documentation, how should I implement user authentication?"

Claude Code will search your indexed docs and provide answers with specific references.

### Example Search

```bash
conduit kb search "authentication flow"

# Output:
# Results for "authentication flow" (3 hits):
#
# 1. [0.92] ~/projects/myapp/docs/auth/oauth-flow.md
#    "The authentication flow begins when the user clicks 'Login'.
#     First, we redirect to the OAuth provider..."
#
# 2. [0.85] ~/projects/myapp/api-specs/auth-endpoints.yaml
#    "POST /auth/login - Initiates the authentication flow.
#     Request body: { email, password }..."
#
# 3. [0.78] ~/projects/myapp/architecture/security.md
#    "Authentication flow diagram shows the complete journey
#     from login to session creation..."
```

### Tips

- Keep documentation in markdown format for best results
- Re-sync periodically: `conduit kb sync`
- Use specific queries for better results

---

## Use Case 2: Multi-Repository Code Search

### Scenario
You work with multiple related repositories and want your AI assistant to understand the full context across all of them.

### Setup

```bash
# Add multiple repositories as sources
conduit kb add ~/repos/frontend --name "Frontend App"
conduit kb add ~/repos/backend --name "Backend API"
conduit kb add ~/repos/shared-libs --name "Shared Libraries"
conduit kb add ~/repos/infrastructure --name "Infrastructure"

# Sync everything
conduit kb sync
```

### File Type Considerations

Conduit indexes text files. For code repositories:

| File Type | Indexed | Notes |
|-----------|---------|-------|
| `.ts`, `.js`, `.py`, `.go` | Yes | Source code |
| `.md`, `.txt` | Yes | Documentation |
| `.json`, `.yaml` | Yes | Configuration |
| `.png`, `.jpg` | No | Binary files skipped |
| `node_modules/` | Excluded | Add to .gitignore pattern |

### Example Workflow

```bash
# Search for how an API endpoint is used
conduit kb search "createUser endpoint"

# Output:
# 1. [0.95] ~/repos/backend/src/routes/users.ts
#    "router.post('/users', createUser) // Creates a new user..."
#
# 2. [0.88] ~/repos/frontend/src/api/users.ts
#    "export const createUser = (data: UserInput) =>
#     api.post('/users', data)..."
#
# 3. [0.82] ~/repos/shared-libs/types/user.ts
#    "export interface CreateUserRequest { email: string; name: string; }"
```

Now Claude Code can see how the API is defined, called, and typed across your entire stack.

---

## Use Case 3: Database Query Assistant

### Scenario
You have a PostgreSQL database and want to give your AI assistant context about the schema without exposing actual data.

### Setup

```bash
# Export your database schema
pg_dump --schema-only mydb > ~/db-context/schema.sql

# Export sample queries
mkdir -p ~/db-context/queries
cp ~/projects/myapp/sql/*.sql ~/db-context/queries/

# Document your ERD
echo "# Database Entity Relationship Diagram

## Users Table
- id: UUID, primary key
- email: VARCHAR(255), unique
- created_at: TIMESTAMP

## Orders Table
- id: UUID, primary key
- user_id: UUID, foreign key to users
- total: DECIMAL(10,2)
- status: ENUM('pending', 'paid', 'shipped')
" > ~/db-context/erd.md

# Add to Conduit
conduit kb add ~/db-context --name "Database Context"
conduit kb sync
```

### Usage

Now you can ask Claude Code:

> "Write a query to find all users who have orders with status 'pending'"

Claude Code will reference your schema and write accurate SQL:

```sql
SELECT DISTINCT u.id, u.email
FROM users u
JOIN orders o ON o.user_id = u.id
WHERE o.status = 'pending';
```

### Security Note

Never export actual data - only schema and structure!

---

## Use Case 4: API Testing & Documentation

### Scenario
You want to give your AI assistant context about your API for writing tests and documentation.

### Setup

```bash
# Create API context directory
mkdir -p ~/api-context

# Export OpenAPI spec
cp ~/projects/myapp/openapi.yaml ~/api-context/

# Add example requests/responses
mkdir ~/api-context/examples
echo '{
  "endpoint": "POST /api/users",
  "request": {
    "email": "test@example.com",
    "name": "Test User"
  },
  "response": {
    "id": "uuid",
    "email": "test@example.com",
    "name": "Test User",
    "created_at": "2024-01-01T00:00:00Z"
  }
}' > ~/api-context/examples/create-user.json

# Add to Conduit
conduit kb add ~/api-context --name "API Context"
conduit kb sync
```

### Example Workflow

Ask Claude Code:

> "Write a test for the user creation endpoint using Jest"

Claude Code generates:

```typescript
import { api } from './test-utils';

describe('POST /api/users', () => {
  it('creates a new user', async () => {
    const response = await api.post('/api/users', {
      email: 'test@example.com',
      name: 'Test User',
    });

    expect(response.status).toBe(201);
    expect(response.body).toMatchObject({
      email: 'test@example.com',
      name: 'Test User',
    });
    expect(response.body.id).toBeDefined();
    expect(response.body.created_at).toBeDefined();
  });
});
```

---

## Use Case 5: Log Analysis Helper

### Scenario
You have log files from various services and want AI assistance in analyzing patterns and debugging issues.

### Setup

```bash
# Create a directory for sanitized logs
mkdir -p ~/log-samples

# Copy relevant log samples (removing sensitive data)
grep -v "password\|secret\|token" /var/log/myapp/error.log | head -1000 > ~/log-samples/errors.log

# Add log format documentation
echo "# Log Format Documentation

## Standard Log Entry
\`\`\`
TIMESTAMP LEVEL [SERVICE] MESSAGE
2024-01-01T10:30:00Z ERROR [auth-service] Failed to validate token
\`\`\`

## Error Codes
- E001: Authentication failed
- E002: Database connection error
- E003: External API timeout
- E004: Invalid request format
" > ~/log-samples/log-formats.md

# Add to Conduit
conduit kb add ~/log-samples --name "Log Samples"
conduit kb sync
```

### Usage

Ask Claude Code:

> "What patterns do you see in our error logs that might indicate a systemic issue?"

Claude Code can analyze:
- Error frequency patterns
- Correlation between error types
- Time-based patterns
- Suggest debugging approaches

### Security Warning

Always sanitize logs before indexing:
- Remove passwords, tokens, API keys
- Mask PII (emails, IPs, names)
- Remove internal URLs and endpoints

---

## Use Case 6: Cloud Infrastructure Context

### Scenario
You manage cloud infrastructure and want your AI assistant to understand your setup.

### Setup

```bash
# Create infrastructure context
mkdir -p ~/infra-context

# Export Terraform state summary (not actual state!)
terraform show -json | jq '.values.root_module.resources[] | {type, name, provider}' > ~/infra-context/resources.json

# Document your architecture
echo "# Cloud Architecture

## AWS Services Used
- ECS for container orchestration
- RDS PostgreSQL for database
- S3 for static assets
- CloudFront for CDN
- Route53 for DNS

## Environments
- Production: us-east-1
- Staging: us-west-2
- Development: local Docker Compose

## Naming Conventions
- Resources: {env}-{service}-{resource}
- Example: prod-api-ecs-cluster
" > ~/infra-context/architecture.md

# Add Terraform modules documentation
cp -r ~/projects/infra/docs ~/infra-context/terraform-docs

# Add to Conduit
conduit kb add ~/infra-context --name "Infrastructure"
conduit kb sync
```

### Usage

Ask Claude Code:

> "How should I add a new Redis cache to our production environment?"

Claude Code can reference your naming conventions, existing resources, and architecture patterns to provide consistent recommendations.

---

## Use Case 7: Research Paper Knowledge Base

### Scenario
You're working on an ML project and want to reference academic papers and research notes.

### Setup

```bash
# Create research context
mkdir -p ~/research-kb

# Add your research notes
cp ~/research/notes/*.md ~/research-kb/

# Add paper summaries (PDFs won't be indexed, so create summaries)
echo "# Paper: Attention Is All You Need (2017)

## Key Concepts
- Transformer architecture
- Self-attention mechanism
- Positional encoding

## Relevant Equations
- Attention(Q,K,V) = softmax(QK^T/√d_k)V

## Implementation Notes
- Use scaled dot-product attention
- Multi-head attention allows parallel processing
- Position-wise feed-forward networks
" > ~/research-kb/transformer-paper.md

# Add to Conduit
conduit kb add ~/research-kb --name "ML Research"
conduit kb sync
```

### Usage

Ask Claude Code:

> "How does the attention mechanism work in transformers, and how should I implement it?"

Claude Code can reference your research summaries and provide implementation guidance based on your notes.

---

## Use Case 8: Team Wiki Integration

### Scenario
Your team has a wiki or documentation site, and you want to make it searchable by your AI assistant.

### Setup

```bash
# Clone or sync your wiki locally
git clone https://github.com/myteam/wiki.git ~/team-wiki

# Or for Confluence, export to markdown
# (Use a Confluence-to-markdown exporter tool)

# Add to Conduit
conduit kb add ~/team-wiki --name "Team Wiki"
conduit kb sync
```

### Periodic Sync

Set up a cron job to keep wiki in sync:

```bash
# Add to crontab (crontab -e)
0 */4 * * * cd ~/team-wiki && git pull && /path/to/conduit kb sync
```

### Usage

Ask Claude Code:

> "What's our team's code review process?"

Claude Code searches the wiki and provides the documented process.

---

## Best Practices

### 1. Organize Your Sources

```
~/conduit-sources/
├── project-docs/       # Current project documentation
├── api-specs/          # API specifications
├── architecture/       # Architecture decision records
├── runbooks/           # Operational runbooks
├── research/           # Research notes and papers
└── team-standards/     # Coding standards and guides
```

### 2. Keep Sources Fresh

```bash
# Create a sync script
#!/bin/bash
# sync-conduit.sh

echo "Syncing Conduit knowledge base..."

# Pull latest from git repos
for repo in ~/conduit-sources/*/; do
    if [ -d "$repo/.git" ]; then
        echo "Updating $repo"
        (cd "$repo" && git pull)
    fi
done

# Re-sync with Conduit
conduit kb sync

echo "Sync complete!"
```

### 3. Exclude Sensitive Content

Create a `.conduitignore` in source directories:

```
# .conduitignore
*.env
*.key
*.pem
secrets/
credentials/
node_modules/
.git/
```

### 4. Use Descriptive Source Names

```bash
# Good
conduit kb add ~/docs --name "Product Requirements 2024"

# Bad
conduit kb add ~/docs --name "Docs"
```

### 5. Monitor KB Size

```bash
# Check statistics regularly
conduit kb stats

# If too large, remove stale sources
conduit kb list
conduit kb remove <old-source-id>
```

---

## Troubleshooting Use Cases

### "Search returns too many irrelevant results"

- Use more specific queries
- Reduce chunk overlap in config
- Remove irrelevant sources

### "Can't find content I know exists"

- Re-sync the source: `conduit kb sync <source-id>`
- Check file encoding (must be UTF-8)
- Verify file type is supported

### "Search is slow"

- Reduce max_results in config
- Check database size with `conduit kb stats`
- Consider removing large, infrequently-used sources

---

## Summary

Conduit's knowledge base turns your local documentation, code, and notes into a searchable context for your AI assistant. By carefully curating what you index and keeping it organized, you can dramatically improve the relevance and accuracy of AI-assisted coding.

**Key Takeaways:**
1. Index documentation, not just code
2. Sanitize before indexing (remove secrets, PII)
3. Keep sources organized and fresh
4. Use specific queries for better results
5. Monitor and maintain your knowledge base
