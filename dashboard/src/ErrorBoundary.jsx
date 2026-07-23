import React from 'react';

/**
 * Catches a render-time throw anywhere below it and shows what happened.
 *
 * Without this, one bad component unmounts the whole tree and React leaves an empty
 * <div id="root"> behind — a white screen with the real error visible only in a console
 * nobody has open. That is the worst failure mode a dashboard can have during a demo,
 * because it looks identical to "the server is down" when the server is fine.
 *
 * The rest of the dashboard is careful to distinguish measured values from modelled ones;
 * this extends the same idea to failure: say what broke, rather than showing nothing and
 * letting the viewer guess.
 */
export default class ErrorBoundary extends React.Component {
  constructor(props) {
    super(props);
    this.state = { error: null, info: null };
  }

  static getDerivedStateFromError(error) {
    return { error };
  }

  componentDidCatch(error, info) {
    this.setState({ info });
    // Keep the console copy: the overlay truncates, and a stack is worth having.
    console.error('[dashboard] render failed:', error, info?.componentStack);
  }

  render() {
    if (!this.state.error) return this.props.children;

    const { error, info } = this.state;
    const box = {
      position: 'fixed', inset: 0, zIndex: 99999, overflow: 'auto',
      background: '#0b0e14', color: '#e6e6e6', padding: '32px 36px',
      font: '13px/1.6 ui-monospace, SFMono-Regular, Menlo, monospace',
    };
    return (
      <div style={box}>
        <div style={{ font: '600 15px/1.4 system-ui, sans-serif', color: '#ff6b6b', marginBottom: 6 }}>
          The dashboard failed to render.
        </div>
        <div style={{ color: '#9aa4b2', marginBottom: 20, fontFamily: 'system-ui, sans-serif' }}>
          The engine may be perfectly healthy — this is a UI fault. Details below.
        </div>
        <div style={{ color: '#ffd479', whiteSpace: 'pre-wrap', marginBottom: 18 }}>
          {String(error && (error.stack || error.message || error))}
        </div>
        {info?.componentStack && (
          <>
            <div style={{ color: '#6b7280', marginBottom: 6 }}>component stack</div>
            <div style={{ color: '#8fb8de', whiteSpace: 'pre-wrap' }}>{info.componentStack}</div>
          </>
        )}
        <button
          type="button"
          onClick={() => this.setState({ error: null, info: null })}
          style={{
            marginTop: 24, padding: '8px 16px', cursor: 'pointer',
            background: '#1b2230', color: '#e6e6e6',
            border: '1px solid #2f3a4d', borderRadius: 4, font: 'inherit',
          }}
        >
          Retry render
        </button>
      </div>
    );
  }
}
