import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  build: { outDir: 'dist', chunkSizeWarningLimit: 1500 },
  server: {
    port: 5173,
    proxy: {
      '/api': { target: 'http://localhost:8317', changeOrigin: true },
    },
  },
});
