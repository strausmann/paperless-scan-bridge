// Conventional Commits enforcement. Scope catalog: .github/SCOPES.md
module.exports = {
  extends: ['@commitlint/config-conventional'],
  // Dependabot always writes its subject as "Bump <dep> from <old> to <new>"
  // (capitalized, sentence-case) — that trips subject-case below, which we
  // otherwise want to keep strict for everyone else. Exempt only that exact
  // shape instead of loosening subject-case globally (#23, #24).
  ignores: [(message) => /^\w+(\([\w.-]+\))?!?:\s*Bump\b.*\bfrom\b.*\bto\b/.test(message.split('\n')[0])],
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
        'scan-bridge', 'sane-runtime', 'scan-processor', 'api', 'profiles', 'tag', 'dispatch',
        'destinations', 'jobs', 'config', 'metrics', 'deploy', 'docker', 'firmware', 'ci', 'docs',
        'deps', 'release',
      ],
    ],
    'subject-case': [2, 'never', ['sentence-case', 'start-case', 'pascal-case', 'upper-case']],
    'header-max-length': [2, 'always', 120],
  },
};
