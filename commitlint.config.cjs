// Conventional Commits enforcement. Scope catalog: .github/SCOPES.md
module.exports = {
  extends: ['@commitlint/config-conventional'],
  rules: {
    'type-enum': [
      2,
      'always',
      ['feat', 'fix', 'perf', 'refactor', 'docs', 'test', 'build', 'ci', 'chore', 'style', 'revert'],
    ],
    'scope-empty': [2, 'never'],
    'scope-enum': [
      2,
      'always',
      [
        'scan-bridge', 'sane-runtime', 'scan-processor', 'api', 'profiles', 'dispatch', 'jobs',
        'config', 'metrics', 'deploy', 'docker', 'ci', 'docs', 'deps', 'release',
      ],
    ],
    'subject-case': [2, 'never', ['sentence-case', 'start-case', 'pascal-case', 'upper-case']],
    'header-max-length': [2, 'always', 120],
  },
};
