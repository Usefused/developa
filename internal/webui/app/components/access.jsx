import {useState} from 'react';
import {useSession} from '../hooks/use-session.jsx';
import {Button,ErrorNotice} from './common.jsx';

export function Access() {
  const {status,error,connect} = useSession();
  const [token,setToken] = useState('');
  if (status === 'connecting') return <p role="status">Connecting to your server…</p>;
  if (status !== 'locked') return <section className="access-panel"><ErrorNotice error={error}/><h1>Connect a repository.</h1>
    <p>Configure a local checkout on the server, then restart it.</p><pre className="setup-code">REPOSITORY_PATH=/absolute/path/to/repo{'\n'}DEVELOPA_API_TOKEN=&lt;at-least-24-characters&gt;</pre>
    <Button onClick={()=>connect('')}>Check connection</Button></section>;
  return <section className="access-panel"><ErrorNotice error={error}/><span className="section-kicker">YOUR CODE STAYS YOURS</span><h1>Open your workspace.</h1>
    <p>The explorer connects directly to your self-hosted server. Enter its access token to see your repository.</p>
    <form onSubmit={event=>{event.preventDefault();connect(token.trim());setToken('');}}>
      <label htmlFor="token">Server access token</label><input id="token" type="password" autoComplete="off" required value={token} onChange={event=>setToken(event.target.value)} placeholder="DEVELOPA_API_TOKEN"/>
      <button type="submit" className="primary-button">Open workspace →</button><p className="form-note">Saved in this browser on this site. Use Lock workspace to forget the token. Avoid signing in on shared computers.</p>
    </form></section>;
}
