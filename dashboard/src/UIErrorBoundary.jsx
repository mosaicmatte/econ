import React from 'react';

// A generic error boundary for complex UI components like React Flow topologies.
// It catches rendering crashes and displays a clean failsafe UI instead of bringing
// down the entire application or rendering a blank white screen.
export default class UIErrorBoundary extends React.Component {
  constructor(props) {
    super(props);
    this.state = { errored: false, error: null };
  }

  static getDerivedStateFromError(error) {
    return { errored: true, error };
  }

  componentDidCatch(error, errorInfo) {
    console.error(`[UIErrorBoundary] caught in ${this.props.name || 'Component'}:`, error, errorInfo);
  }

  render() {
    if (this.state.errored) {
      return (
        <div style={{ position: 'absolute', inset: 0, display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', background: '#0a0a0c', color: '#fff', zIndex: 100, textAlign: 'center', padding: '20px', borderRadius: '8px', border: '1px solid rgba(255, 149, 0, 0.3)' }}>
          <div style={{ marginBottom: '16px', background: 'rgba(255, 149, 0, 0.2)', padding: '12px', borderRadius: '50%' }}>
            <svg width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="#FF9500" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><circle cx="12" cy="12" r="10"></circle><line x1="12" y1="8" x2="12" y2="12"></line><line x1="12" y1="16" x2="12.01" y2="16"></line></svg>
          </div>
          <h3 style={{ fontSize: '16px', fontWeight: '600', margin: '0 0 8px' }}>{this.props.name || 'Component'} Offline</h3>
          <p style={{ fontSize: '13px', color: 'rgba(255,255,255,0.6)', margin: '0 0 20px', maxWidth: '250px' }}>
            A rendering fault occurred. Background data processing remains fully active.
          </p>
          <button 
            onClick={() => this.setState({ errored: false, error: null })}
            style={{ background: '#333', border: '1px solid #444', padding: '8px 16px', borderRadius: '6px', color: '#fff', fontSize: '12px', fontWeight: '600', cursor: 'pointer', transition: 'background 0.2s' }}
          >
            Attempt Recovery
          </button>
        </div>
      );
    }
    
    return this.props.children;
  }
}
