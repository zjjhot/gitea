export default {
  exclude: [
    '@mcaptcha/vanilla-glue', // breaking changes in rc versions need to be handled
    'eslint', // need to migrate to eslint flat config first
    'eslint-plugin-array-func', // need to migrate to eslint flat config first
    'eslint-plugin-vitest', // need to migrate to eslint flat config first
  ],
};
