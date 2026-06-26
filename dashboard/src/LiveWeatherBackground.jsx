import React, { useState, useEffect } from 'react';

// Using Open-Meteo for free, no-key live weather data. Defaulting to Ho Chi Minh City.
const WEATHER_API_URL = 'https://api.open-meteo.com/v1/forecast?latitude=10.8231&longitude=106.6297&current=temperature_2m,cloud_cover,is_day,precipitation,weather_code';

export default function LiveWeatherBackground() {
  const [weather, setWeather] = useState(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let mounted = true;
    const fetchWeather = async () => {
      try {
        const response = await fetch(WEATHER_API_URL);
        const data = await response.json();
        if (mounted) setWeather(data.current);
      } catch (err) {
        console.error('Failed to fetch weather', err);
        // Fallback to clear night if API fails
        if (mounted) setWeather({ is_day: 0, cloud_cover: 0, precipitation: 0, weather_code: 0 });
      } finally {
        if (mounted) setLoading(false);
      }
    };
    fetchWeather();
    // Refresh weather every 15 minutes
    const interval = setInterval(fetchWeather, 15 * 60 * 1000);
    return () => {
      mounted = false;
      clearInterval(interval);
    };
  }, []);

  if (loading || !weather) {
    return <div style={{ position: 'absolute', inset: 0, background: '#0a0a0c', zIndex: 0 }} />;
  }

  const { is_day, cloud_cover, precipitation } = weather;

  // Determine ambient gradients based on exact state
  let ambientStyle = {};
  if (is_day === 1) {
    if (cloud_cover > 70 || precipitation > 0) {
      // Overcast / Stormy Day
      ambientStyle = {
        background: `
          radial-gradient(circle at 20% 20%, rgba(130, 150, 180, 0.4), transparent 50%),
          radial-gradient(circle at 80% 80%, rgba(100, 120, 150, 0.3), transparent 50%),
          linear-gradient(180deg, #1a2230 0%, #11141c 100%)
        `
      };
    } else {
      // Clear / Partly Cloudy Day
      ambientStyle = {
        background: `
          radial-gradient(circle at 20% 20%, rgba(110, 180, 255, 0.5), transparent 50%),
          radial-gradient(circle at 80% 80%, rgba(88, 128, 255, 0.3), transparent 50%),
          linear-gradient(180deg, #13254a 0%, #0b1020 100%)
        `
      };
    }
  } else {
    // Night
    ambientStyle = {
      background: `
        radial-gradient(circle at 20% 20%, rgba(50, 80, 140, 0.2), transparent 50%),
        radial-gradient(circle at 80% 80%, rgba(30, 60, 100, 0.15), transparent 50%),
        linear-gradient(180deg, #090c14 0%, #040508 100%)
      `
    };
  }

  return (
    <>
      <style>
        {`
          .weather-bg-ambient {
            position: absolute;
            inset: 0;
            overflow: hidden;
            pointer-events: none;
            z-index: 0;
            transition: background 3s ease;
          }
          
          /* Safe, GPU-accelerated slow drift */
          @keyframes safeDrift {
            0% { transform: scale(1) translate3d(0, 0, 0); }
            50% { transform: scale(1.05) translate3d(2%, 2%, 0); }
            100% { transform: scale(1) translate3d(0, 0, 0); }
          }
          
          .weather-bg-animated-layer {
            position: absolute;
            inset: -10%;
            width: 120%;
            height: 120%;
            pointer-events: none;
            animation: safeDrift 25s ease-in-out infinite alternate;
            will-change: transform;
          }
        `}
      </style>
      
      {/* Container to handle overflow */}
      <div className="weather-bg-ambient" style={{
        backgroundImage: 'url(/weather_sky_gradient.png)',
        backgroundSize: 'cover',
        backgroundPosition: 'top right',
        backgroundRepeat: 'no-repeat'
      }}>
        {/* Layer 1: Ambient CSS Gradient (Apple style) that drifts safely */}
        <div className="weather-bg-animated-layer" style={{...ambientStyle, opacity: 0.1}} />
      </div>

      {/* Weather info text removed per user request */}
    </>
  );
}
