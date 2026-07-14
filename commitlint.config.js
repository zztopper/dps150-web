// Conventional Commits — обязательный формат commit messages.
// Установить: cd <project> && npm install -D @commitlint/cli @commitlint/config-conventional husky
// Hook:       npx husky add .husky/commit-msg 'npx --no -- commitlint --edit $1'
//
// Формат: <type>(<scope>): <subject>
// Examples:
//   feat(api): add /export endpoint for PDF
//   fix(auth): correct token expiry calculation
//   refactor(repo): extract user lookup helper
//   docs(architecture): publish ADR-007
//   test(integration): cover migration rollback paths
//   chore(deps): bump go version to 1.25
//
// Allowed types:
//   feat:     новая функциональность
//   fix:      bug-fix
//   refactor: рефакторинг без изменения поведения
//   perf:     оптимизация производительности
//   test:     добавление/правка тестов
//   docs:     документация (включая ADR, CHANGELOG)
//   build:    система сборки, deps
//   ci:       CI-конфиги
//   chore:    рутина, не попадающая в release notes
//   revert:   revert предыдущего коммита

module.exports = {
  extends: ['@commitlint/config-conventional'],
  rules: {
    'subject-case': [2, 'never', ['upper-case']],
    'subject-max-length': [2, 'always', 100],
    'body-max-line-length': [1, 'always', 120],
    'type-enum': [
      2,
      'always',
      ['feat', 'fix', 'refactor', 'perf', 'test', 'docs', 'build', 'ci', 'chore', 'revert'],
    ],
    'scope-empty': [1, 'never'],
  },
};
