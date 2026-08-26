import {index,layout,route} from '@react-router/dev/routes';

export default [index('./routes/home.jsx'),layout('./routes/workspace.jsx', [
  route('blocks','./routes/blocks.jsx'),
  route('flow','./routes/flow.jsx'),
  route('changes','./routes/changes.jsx'),
  route('analysis','./routes/analysis.jsx'),
  route('features','./routes/features.jsx'),
  route('chain','./routes/chain.jsx'),
  route('*','./routes/not-found.jsx'),
])];
