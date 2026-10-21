// @ts-check
import js from '@eslint/js';
import { defineConfig } from 'eslint/config';
import reactHooks from 'eslint-plugin-react-hooks';
import tseslint from 'typescript-eslint';

export default defineConfig(
  { ignores: ['dist', 'node_modules', 'src/api/schema.d.ts', 'playwright-report', 'test-results'] },
  js.configs.recommended,
  tseslint.configs.strictTypeChecked,
  {
    languageOptions: {
      parserOptions: {
        projectService: { allowDefaultProject: ['eslint.config.js'] },
        tsconfigRootDir: import.meta.dirname,
      },
    },
    plugins: { 'react-hooks': reactHooks },
    rules: {
      ...reactHooks.configs.recommended.rules,
      '@typescript-eslint/no-explicit-any': 'error',
      '@typescript-eslint/restrict-template-expressions': ['error', { allowNumber: true }],
      // XSS by construction (PLAN §7.1): no HTML strings anywhere.
      'no-restricted-syntax': [
        'error',
        {
          selector: "JSXAttribute[name.name='dangerouslySetInnerHTML']",
          message: 'No dangerouslySetInnerHTML (PLAN §7.1).',
        },
        { selector: "MemberExpression[property.name='innerHTML']", message: 'No innerHTML (PLAN §7.1).' },
        { selector: "MemberExpression[property.name='outerHTML']", message: 'No outerHTML (PLAN §7.1).' },
      ],
    },
  },
  // The config file itself is plain JS: no type-aware rules.
  { files: ['eslint.config.js'], ...tseslint.configs.disableTypeChecked },
);
