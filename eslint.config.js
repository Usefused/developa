import react from 'eslint-plugin-react';

export default [{ignores:['internal/webui/assets/generated/**','internal/webui/dist/**','internal/webui/.build/**']},{
  files: ['internal/webui/**/*.js','internal/webui/**/*.jsx'],
  plugins:{react},
  languageOptions: {
    ecmaVersion: 'latest', sourceType: 'module', parserOptions:{ecmaFeatures:{jsx:true}},
    globals: Object.fromEntries(['document','window','fetch','Response','URLSearchParams','AbortController','setTimeout','clearTimeout','console','localStorage','navigator','Intl'].map(name=>[name,'readonly'])),
  },
  rules: {
    'react/jsx-uses-vars':'error',
    complexity: ['error',10], 'no-unused-vars': 'error', 'no-undef': 'error',
    eqeqeq: 'error', 'no-eval': 'error', 'no-implied-eval': 'error',
  },
},{
  files: ['api/*.mjs'],
  languageOptions: {ecmaVersion:'latest', sourceType:'module', globals:{URL:'readonly', structuredClone:'readonly', console:'readonly'}},
  rules: {complexity:['error',10], 'no-unused-vars':'error', 'no-undef':'error', eqeqeq:'error', 'no-eval':'error'},
}];
