# Contributing to Korik

Thank you for your interest in contributing to Korik! This document provides guidelines and standards for contributing.

## Code of Conduct

Be respectful, constructive, and professional in all interactions.

## Development Setup

### Prerequisites

- Go 1.21 or higher
- Git
- golangci-lint (for linting)

### Setup

```bash
# Install dependencies
go mod download

# Run the application
go run . -name your-name
```

## Commit Message Guidelines

We follow [Conventional Commits](https://www.conventionalcommits.org/) specification.

### Format

```
<type>(scope): <subject>

<body>

<footer>
```

### Types

- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation changes
- `style`: Code style/formatting (no logic changes)
- `refactor`: Code refactoring
- `perf`: Performance improvements
- `test`: Adding or updating tests
- `build`: Build system changes
- `ci`: CI/CD changes
- `chore`: Maintenance tasks
- `revert`: Reverting changes

### Scopes

Common scopes in Korik:

- `core`: Core functionality
- `peer`: P2P networking
- `crypto`: Encryption/security
- `identity`: Identity management
- `nat`: NAT traversal/STUN
- `gui`: Web interface
- `cli`: Command-line interface
- `discovery`: Peer discovery
- `transport`: Transport layer

### Examples

```bash
feat(nat): add STUN server support for NAT traversal

Implement STUN client to discover public IP and enable P2P 
connections across different networks via UDP hole punching.

Closes #42
```

```bash
fix(crypto): prevent panic on invalid public key format

Add validation for public key hex strings before decoding
to prevent runtime panics.
```

```bash
docs(api): add peer connection examples
```

## Pull Request Process

1. **Create a branch** from `main`:
   ```bash
   git checkout -b feat/your-feature-name
   ```
2. **Make your changes** following our coding standards
3. **Write tests** for new functionality
4. **Run tests**:
   ```bash
   go test ./...
   ```
6. **Run linter**:
   ```bash
   golangci-lint run
   ```
7. **Commit** using conventional commits:
   ```bash
   git commit -m "feat(scope): your feature description"
   ```
8. **Push** to your fork:
   ```bash
   git push origin feat/your-feature-name
   ```
9. **Open a Pull Request** against `main` branch

### PR Requirements

- [ ] Follows conventional commit format
- [ ] Includes tests for new features
- [ ] All tests pass
- [ ] Linter passes with no errors
- [ ] Documentation updated if needed
- [ ] PR description clearly explains the changes

## Coding Standards

### Go Style

- Follow [Effective Go](https://go.dev/doc/effective_go)
- Use `gofmt` for formatting
- Keep functions focused and small
- Write clear, self-documenting code
- Add comments for complex logic

### Testing

- Write table-driven tests
- Test edge cases and error conditions
- Aim for meaningful coverage, not just high percentages

### Security

- Never commit secrets or credentials
- Validate all external inputs
- Follow secure coding practices for cryptographic operations

## Project Structure

```
korik/
├── internal/
│   ├── peer/         # P2P networking
│   ├── crypto/       # Encryption
│   ├── identity/     # Identity management
│   ├── nat/          # NAT traversal
│   └── gui/          # Web interface
├── .github/          # GitHub Actions workflows
├── main.go           # Entry point
└── go.mod
```

## Running Tests

```bash
# Run all tests
go test ./...

# Run with coverage
go test -cover ./...

# Run with race detection
go test -race ./...

# Run specific package
go test ./internal/crypto
```

## Running Linter

```bash
# Run golangci-lint
golangci-lint run

# Auto-fix issues when possible
golangci-lint run --fix
```

## Release Process

Releases are automated via semantic-release based on commit messages:

- `feat:` commits → minor version bump (0.x.0)
- `fix:`, `perf:` commits → patch version bump (0.0.x)
- `BREAKING CHANGE:` in footer → major version bump (x.0.0)

## Questions?

Contact the project maintainer or open an issue.

## License

By contributing, you agree that your contributions will be licensed under the same license as the project.
