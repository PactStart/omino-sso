import pluginVue from 'eslint-plugin-vue'
import vueTsEslintConfig from '@vue/eslint-config-typescript'
import globals from 'globals'

/**
 * ESLint 扁平配置（eslint 10 + vue 10 + typescript-eslint 8）。
 * 只做"能真实失败"的门禁：语法/未定义变量/未使用变量/Vue 模板必备规则，
 * 不引入样式类规则（格式由编辑器与 vue-tsc 负责），避免与既有代码风格冲突。
 */
export default [
  {
    ignores: ['dist/**', 'node_modules/**', '*.timestamp-*'],
  },
  ...pluginVue.configs['flat/essential'],
  ...vueTsEslintConfig(),
  {
    languageOptions: {
      globals: {
        ...globals.browser,
      },
    },
    rules: {
      // 视图文件名是 Login/Account/Apps 这类单词，属约定俗成
      'vue/multi-word-component-names': 'off',
      // 允许在 catch/错误处理中显式标注 any，但提示
      '@typescript-eslint/no-explicit-any': 'warn',
      // 空 catch 用于吞掉非关键异常（如 sessionStorage 不可用），允许带注释的空块
      'no-empty': ['error', { allowEmptyCatch: true }],
    },
  },
]
