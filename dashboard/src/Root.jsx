import React, { useState, useEffect } from 'react';
import App from './App';
import MobileApp from './MobileApp';
import ErrorBoundary from './ErrorBoundary';

export default function Root() {
  const [isMobile, setIsMobile] = useState(window.innerWidth < 768);

  useEffect(() => {
    const handleResize = () => {
      setIsMobile(window.innerWidth < 768);
    };
    
    window.addEventListener('resize', handleResize);
    return () => window.removeEventListener('resize', handleResize);
  }, []);

  // One boundary around the whole tree: a throw in any panel shows what broke instead of
  // unmounting everything and leaving a white screen that looks like the engine is down.
  return (
    <ErrorBoundary>
      {isMobile ? <MobileApp /> : <App />}
    </ErrorBoundary>
  );
}
