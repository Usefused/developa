import {Navigate,useLocation} from 'react-router';
import {homeURL} from '../lib/routes.js';
export default function HomeRoute() { return <Navigate replace to={homeURL(useLocation().search)}/>; }
