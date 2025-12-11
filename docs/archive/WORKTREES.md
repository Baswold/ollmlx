# Git Worktrees Setup for ollmlx

## What Are Worktrees?

Git worktrees allow you to have multiple working directories for the same repository, each on a different branch. This is perfect for working on different aspects of ollmlx simultaneously.

## Current Worktrees

You now have 4 worktrees:

1. **Main Worktree** (`/Users/basil_jackson/Documents/Ollama-MLX`)
   - Branch: `main`
   - Purpose: Main development, documentation, coordination

2. **Go Integration** (`/Users/basil_jackson/Documents/ollama-mlx-go-integration`)
   - Branch: `go-integration`
   - Purpose: Implementing the Go ↔ MLX backend integration
   - Files to create/modify:
     - `runner/mlxrunner/runner.go` (NEW)
     - `llm/server.go` (MODIFY)
     - `llm/detection.go` (NEW)

3. **Model Management** (`/Users/basil_jackson/Documents/ollama-mlx-model-management`)
   - Branch: `model-management`
   - Purpose: Hugging Face model discovery and lifecycle
   - Files to create/modify:
     - `server/models.go` (NEW)
     - `server/routes.go` (MODIFY)
     - Model metadata and caching logic

4. **Testing** (`/Users/basil_jackson/Documents/ollama-mlx-testing`)
   - Branch: `testing`
   - Purpose: Integration tests, compatibility tests, benchmarks
   - Files to create:
     - `integration/mlx_test.go`
     - `integration/compatibility_test.go`
     - Test infrastructure

## How to Use Worktrees

### Switching Between Worktrees

```bash
# Go to Go Integration worktree
cd /Users/basil_jackson/Documents/ollama-mlx-go-integration

# Go to Model Management worktree
cd /Users/basil_jackson/Documents/ollama-mlx-model-management

# Go to Testing worktree
cd /Users/basil_jackson/Documents/ollama-mlx-testing

# Go back to main worktree
cd /Users/basil_jackson/Documents/Ollama-MLX
```

### Creating New Worktrees

```bash
# Create a new worktree for documentation
cd /Users/basil_jackson/Documents/Ollama-MLX
git worktree add ../ollama-mlx-docs -b documentation main
```

### Removing Worktrees

```bash
# First, go to the worktree and commit or stash changes
cd /Users/basil_jackson/Documents/ollama-mlx-go-integration
git commit -a -m "Work in progress"

# Then remove the worktree
cd /Users/basil_jackson/Documents/Ollama-MLX
git worktree remove /Users/basil_jackson/Documents/ollama-mlx-go-integration
```

## Worktree Benefits for ollmlx

1. **Isolated Development**: Work on Go integration without affecting model management code
2. **Parallel Progress**: Different team members can work on different aspects simultaneously
3. **Clean Branches**: Each worktree has its own branch with focused changes
4. **Easy Switching**: Quickly context-switch between different parts of the project
5. **No Merge Conflicts**: Since each worktree is on its own branch, you avoid conflicts

## Best Practices

1. **Commit Regularly**: Each worktree should have its own branch with frequent commits
2. **Push Branches**: Push your worktree branches to remote for backup
3. **Clean Worktrees**: Remove worktrees when they're no longer needed
4. **Document Purpose**: Keep track of what each worktree is for (like this file!)
5. **Sync Main**: Regularly pull updates from main into your worktrees

## Current Worktree Status

```
/Users/basil_jackson/Documents/Ollama-MLX                   1c71472 [main]
/Users/basil_jackson/Documents/ollama-mlx-go-integration    1c71472 [go-integration]
/Users/basil_jackson/Documents/ollama-mlx-model-management  1c71472 [model-management]
/Users/basil_jackson/Documents/ollama-mlx-testing           1c71472 [testing]
```

All worktrees are currently at the same commit (1c71472) which includes the `what_i_want.md` documentation.

## Next Steps

1. **Go Integration Worktree**: Start implementing `runner/mlxrunner/runner.go`
2. **Model Management Worktree**: Begin work on Hugging Face integration
3. **Testing Worktree**: Design the test infrastructure
4. **Main Worktree**: Coordinate and document progress

Each worktree can progress independently, then we'll merge them back to main when ready!
