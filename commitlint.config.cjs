// Conventional Commits enforcement. Scope catalog: .github/SCOPES.md
module.exports = {
  extends: ['@commitlint/config-conventional'],
  // Dependabot adds a second scope and puts the version range in its body.
  // Exempt only its generated message format instead of loosening the rules.
  ignores: [
    (message) => {
      const header = message.split('\n')[0];
      return (
        /^\w+(?:\([\w.-]+\))+\!?:\s*Bump\b/.test(header) &&
        /^Bumps \[[^\]]+\]\(https:\/\/github\.com\/[^)\s]+\) from \S+ to \S+\.$/m.test(message) &&
        /^updated-dependencies:$/m.test(message) &&
        /^Signed-off-by: dependabot\[bot\] <support@github\.com>$/m.test(message)
      );
    },
  ],
  rules: {
    'type-enum': [
      2,
      'always',
      [
        'feat',
        'fix',
        'perf',
        'refactor',
        'docs',
        'test',
        'build',
        'ci',
        'chore',
        'style',
        'revert',
      ],
    ],
    'scope-empty': [2, 'never'],
    'scope-enum': [
      2,
      'always',
      [
        'scan-bridge',
        'sane-runtime',
        'scan-processor',
        'api',
        'profiles',
        'tag',
        'dispatch',
        'destinations',
        'jobs',
        'config',
        'metrics',
        'deploy',
        'docker',
        'firmware',
        'ci',
        'docs',
        'deps',
        'release',
      ],
    ],
    'subject-case': [2, 'never', ['sentence-case', 'start-case', 'pascal-case', 'upper-case']],
    'header-max-length': [2, 'always', 120],
  },
};
