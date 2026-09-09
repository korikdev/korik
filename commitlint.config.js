module.exports = {
  extends: ['@commitlint/config-conventional'],
  rules: {
    'type-enum': [
      2,
      'always',
      [
        'feat',     // New feature
        'fix',      // Bug fix
        'docs',     // Documentation
        'style',    // Code style (formatting)
        'refactor', // Refactoring
        'perf',     // Performance
        'test',     // Tests
        'build',    // Build system
        'ci',       // CI/CD
        'chore',    // Maintenance
        'revert',   // Revert commit
        'release',  // Release version
      ],
    ],
    'scope-enum': [
      2,
      'always',
      [
        'core',       // Core functionality
        'cli',        // CLI interface
        'runtime',    // Runtime logic
        'peer',       // P2P networking
        'crypto',     // Encryption
        'identity',   // Identity management
        'nat',        // NAT traversal
        'gui',        // Web GUI
        'discovery',  // Peer discovery
        'transport',  // Transport layer
        'deps',       // Dependencies
        'config',     // Configuration
        'api',        // API
        'build',      // Build system
        'release',    // Release automation
      ],
    ],
    'scope-case': [2, 'always', 'lower-case'],
    'subject-case': [0],
    'subject-empty': [2, 'never'],
    'subject-full-stop': [2, 'never', '.'],
    'header-max-length': [2, 'always', 100],
    'body-leading-blank': [2, 'always'],
    'footer-leading-blank': [2, 'always'],
  },
};
