// Draws the dashboard charts from the JSON events on /events. Everything else
// on the page (stat tiles, match feed, tables, load generator) is server
// rendered HTML swapped in by htmx.
(() => {
  'use strict';

  const MAX_POINTS = 600; // ten minutes of one-second samples
  const root = document.documentElement;

  // ---- theme tokens ------------------------------------------------------
  const css = (name) => getComputedStyle(root).getPropertyValue(name).trim();
  let T = {};
  function readTokens() {
    T = {
      surface: css('--surface'), text: css('--text-primary'), secondary: css('--text-secondary'),
      muted: css('--text-muted'), grid: css('--grid'), axis: css('--axis'), border: css('--border'),
      series: [css('--series-1'), css('--series-2'), css('--series-3')], font: css('--font'),
    };
    Chart.defaults.font.family = T.font;
    Chart.defaults.color = T.secondary;
  }
  const isDark = () => root.dataset.theme === 'dark'
    || (!root.dataset.theme && window.matchMedia('(prefers-color-scheme: dark)').matches);
  const slotColor = (slot) => T.series[(slot - 1) % T.series.length];
  function alpha(hex, a) {
    const n = parseInt(hex.slice(1), 16);
    return `rgba(${n >> 16}, ${(n >> 8) & 255}, ${n & 255}, ${a})`;
  }

  // ---- formatting --------------------------------------------------------
  const fmt = (v, digits = 0) => (v == null || Number.isNaN(v)) ? '–'
    : v.toLocaleString(undefined, { minimumFractionDigits: digits, maximumFractionDigits: digits });
  const clock = (ms) => new Date(ms).toLocaleTimeString([], { hour12: false });
  // fewest decimals (up to max) that still tell every tick apart: 0, 20, 40
  // rather than 0.0, 20.0, 40.0
  const tickFormatter = (max) => (v, _i, ticks) => {
    let d = 0;
    while (d < max && ticks.some((t) => Math.abs(t.value - Number(t.value.toFixed(d))) > 1e-9)) d++;
    return fmt(v, d);
  };
  const get = (obj, path) => path.split('.').reduce((o, k) => (o == null ? undefined : o[k]), obj);

  // ---- plugins -----------------------------------------------------------
  // vertical hairline at the hovered x
  const crosshair = {
    id: 'crosshair',
    afterDatasetsDraw(chart) {
      const active = chart.tooltip && chart.tooltip.getActiveElements();
      if (!active || !active.length) return;
      const { ctx, chartArea: { top, bottom } } = chart;
      const x = active[0].element.x;
      ctx.save();
      ctx.strokeStyle = T.axis;
      ctx.lineWidth = 1;
      ctx.beginPath(); ctx.moveTo(x, top); ctx.lineTo(x, bottom); ctx.stroke();
      ctx.restore();
    },
  };

  // labeled horizontal reference line, e.g. slope = 1
  const refLine = {
    id: 'refLine',
    afterDatasetsDraw(chart, _args, opts) {
      if (opts.value == null) return;
      const { ctx, chartArea: { left, right, top, bottom }, scales: { y } } = chart;
      const py = y.getPixelForValue(opts.value);
      if (py < top || py > bottom) return;
      ctx.save();
      ctx.strokeStyle = T.muted;
      ctx.lineWidth = 1;
      ctx.beginPath(); ctx.moveTo(left, py); ctx.lineTo(right, py); ctx.stroke();
      if (opts.label) {
        ctx.fillStyle = T.secondary;
        ctx.font = `12px ${T.font}`;
        ctx.textBaseline = 'bottom';
        ctx.fillText(opts.label, left + 4, py - 3);
      }
      ctx.restore();
    },
  };

  // latest value at the end of each line; a label that would collide with
  // the one above is left to the legend and tooltip
  const endLabels = {
    id: 'endLabels',
    afterDatasetsDraw(chart, _args, opts) {
      const { ctx } = chart;
      const labels = [];
      chart.data.datasets.forEach((ds, i) => {
        const meta = chart.getDatasetMeta(i);
        for (let j = ds.data.length - 1; j >= 0; j--) {
          if (ds.data[j] == null) continue;
          const el = meta.data[j];
          if (el) labels.push({ x: el.x, y: el.y, color: ds.borderColor, text: fmt(ds.data[j], opts.digits) });
          return;
        }
      });
      labels.sort((a, b) => a.y - b.y);
      let lastY = -Infinity;
      ctx.save();
      ctx.font = `12px ${T.font}`;
      ctx.textBaseline = 'middle';
      for (const l of labels) {
        ctx.beginPath();
        ctx.arc(l.x, l.y, 4, 0, Math.PI * 2);
        ctx.fillStyle = l.color; ctx.fill();
        ctx.lineWidth = 2; ctx.strokeStyle = T.surface; ctx.stroke();
        if (l.y - lastY < 14) continue;
        ctx.fillStyle = T.secondary;
        ctx.fillText(l.text, l.x + 8, l.y);
        lastY = l.y;
      }
      ctx.restore();
    },
  };

  const tooltipStyle = () => ({
    backgroundColor: T.surface, borderColor: T.border, borderWidth: 1,
    titleColor: T.secondary, bodyColor: T.text, padding: 10,
    bodyFont: { weight: '600' }, usePointStyle: true, boxPadding: 4,
  });
  const axisStyle = () => ({
    x: { ticks: { color: T.muted, maxRotation: 0, autoSkipPadding: 24 }, grid: { display: false }, border: { color: T.axis } },
    y: { ticks: { color: T.muted, maxTicksLimit: 5 }, grid: { color: T.grid }, border: { display: false } },
  });

  // ---- charts ------------------------------------------------------------
  const lineCharts = []; // { canvas, spec, chart }
  let scatter = null;
  let histogram = null;
  let samples = [];
  let pointsData = null;

  function buildLineChart(canvas, spec) {
    const axes = axisStyle();
    axes.y.ticks.callback = tickFormatter(spec.digits);
    if (spec.min != null) axes.y.min = spec.min;
    if (spec.max != null) axes.y.max = spec.max;
    if (spec.ref != null) {
      // keep the reference line in view even when the data sits far from it
      axes.y.suggestedMin = spec.ref - 0.1;
      axes.y.suggestedMax = spec.ref + 0.1;
    }
    return new Chart(canvas, {
      type: 'line',
      data: {
        labels: samples.map((s) => clock(s.t)),
        datasets: spec.series.map((s) => ({
          label: s.label,
          data: samples.map((x) => get(x, s.key) ?? null),
          borderColor: slotColor(s.slot), backgroundColor: slotColor(s.slot),
          borderWidth: 2, borderJoinStyle: 'round', borderCapStyle: 'round',
          pointRadius: 0, pointHoverRadius: 5, pointHoverBorderWidth: 2, pointHoverBorderColor: T.surface,
          tension: 0, spanGaps: false,
        })),
      },
      options: {
        animation: false, responsive: true, maintainAspectRatio: false,
        interaction: { mode: 'index', intersect: false },
        layout: { padding: { right: 52, top: 4 } },
        scales: axes,
        plugins: {
          legend: {
            display: spec.series.length > 1, align: 'start',
            labels: { usePointStyle: true, pointStyle: 'line', color: T.secondary, boxWidth: 18 },
          },
          tooltip: {
            ...tooltipStyle(),
            filter: (item) => item.raw != null,
            callbacks: {
              label: (c) => `${fmt(c.parsed.y, spec.digits)}  ${c.dataset.label}`,
              labelPointStyle: () => ({ pointStyle: 'line', rotation: 0 }),
            },
          },
          refLine: { value: spec.ref, label: spec.refLabel },
          endLabels: { digits: spec.digits },
        },
      },
      plugins: [crosshair, refLine, endLabels],
    });
  }

  function buildScatter(canvas) {
    const axes = axisStyle();
    axes.x.type = 'linear';
    axes.x.grid = { color: T.grid };
    axes.x.title = { display: true, text: 'True skill', color: T.secondary };
    axes.y.title = { display: true, text: 'Rating', color: T.secondary };
    axes.x.ticks.callback = axes.y.ticks.callback = (v) => fmt(v);
    return new Chart(canvas, {
      type: 'scatter',
      data: {
        datasets: [
          {
            label: 'Players', data: [], backgroundColor: alpha(slotColor(1), isDark() ? 0.7 : 0.45),
            borderColor: T.surface, borderWidth: 1, pointRadius: 4, pointHoverRadius: 6, pointStyle: 'circle',
          },
          {
            label: 'Rating = true skill', type: 'line', data: [], borderColor: T.muted,
            borderWidth: 1, pointRadius: 0, pointHoverRadius: 0, pointStyle: 'line',
          },
        ],
      },
      options: {
        animation: false, responsive: true, maintainAspectRatio: false,
        interaction: { mode: 'nearest', intersect: false }, // nearest point, not pixel-perfect hits
        scales: axes,
        plugins: {
          legend: { align: 'start', labels: { usePointStyle: true, color: T.secondary } },
          tooltip: {
            ...tooltipStyle(),
            filter: (item) => item.datasetIndex === 0,
            callbacks: {
              title: () => '',
              label: (c) => `${fmt(c.parsed.y)} rating · ${fmt(c.parsed.x)} true skill`,
            },
          },
        },
      },
    });
  }

  function buildHistogram(canvas) {
    const axes = axisStyle();
    axes.y.ticks.precision = 0;
    const bar = (label, slot) => ({
      label, data: [], backgroundColor: slotColor(slot),
      borderRadius: 4, borderSkipped: 'start', maxBarThickness: 24,
      categoryPercentage: 0.8, barPercentage: 0.9,
    });
    return new Chart(canvas, {
      type: 'bar',
      data: { labels: [], datasets: [bar('By rating', 1), bar('By true skill', 2)] },
      options: {
        animation: false, responsive: true, maintainAspectRatio: false,
        interaction: { mode: 'index', intersect: false },
        scales: axes,
        plugins: {
          legend: { align: 'start', labels: { usePointStyle: true, pointStyle: 'rect', color: T.secondary } },
          tooltip: {
            ...tooltipStyle(),
            callbacks: {
              title: (items) => `${items[0].label} – ${fmt(Number(items[0].label.replace(/,/g, '')) + 100)}`,
              label: (c) => `${fmt(c.parsed.y)} players  ${c.dataset.label.toLowerCase()}`,
            },
          },
        },
      },
    });
  }

  function drawPoints() {
    if (!pointsData) return;
    const { points, hist } = pointsData;
    if (scatter) {
      scatter.data.datasets[0].data = points.map((p) => ({ x: p.s, y: p.r }));
      if (points.length) {
        let lo = Infinity, hi = -Infinity;
        for (const p of points) { lo = Math.min(lo, p.r, p.s); hi = Math.max(hi, p.r, p.s); }
        lo = Math.floor(lo / 100) * 100; hi = Math.ceil(hi / 100) * 100;
        // same range on both axes so the reference line sits at 45 degrees
        for (const axis of ['x', 'y']) { scatter.options.scales[axis].min = lo; scatter.options.scales[axis].max = hi; }
        scatter.data.datasets[1].data = [{ x: lo, y: lo }, { x: hi, y: hi }];
      }
      scatter.update('none');
    }
    if (histogram) {
      histogram.data.labels = (hist.bins || []).map((b) => fmt(b));
      histogram.data.datasets[0].data = hist.ratings || [];
      histogram.data.datasets[1].data = hist.skills || [];
      histogram.update('none');
    }
  }

  function buildAll() {
    for (const c of lineCharts) { c.chart && c.chart.destroy(); c.chart = buildLineChart(c.canvas, c.spec); }
    scatter && scatter.destroy();
    histogram && histogram.destroy();
    const sc = document.getElementById('scatter');
    const hc = document.getElementById('histogram');
    scatter = sc ? buildScatter(sc) : null;
    histogram = hc ? buildHistogram(hc) : null;
    drawPoints();
  }

  function addSample(s) {
    samples.push(s);
    if (samples.length > MAX_POINTS) samples.shift();
    for (const { chart, spec } of lineCharts) {
      chart.data.labels.push(clock(s.t));
      spec.series.forEach((series, i) => chart.data.datasets[i].data.push(get(s, series.key) ?? null));
      if (chart.data.labels.length > MAX_POINTS) {
        chart.data.labels.shift();
        chart.data.datasets.forEach((ds) => ds.data.shift());
      }
      chart.update('none');
    }
  }

  // ---- live data ---------------------------------------------------------
  function onSSE(e) {
    const type = e.detail && e.detail.type;
    if (type !== 'history' && type !== 'chart' && type !== 'points') return;
    e.preventDefault(); // JSON, not HTML: draw it instead of swapping
    const data = JSON.parse(e.detail.data);
    if (type === 'history') {
      samples = (data || []).slice(-MAX_POINTS);
      buildAll();
    } else if (type === 'chart') {
      addSample(data);
    } else {
      pointsData = data;
      drawPoints();
    }
  }

  function setLive(state, text) {
    const el = document.getElementById('live');
    el.className = `status status-${state}`;
    el.querySelector('.status-text').textContent = text;
  }

  // ---- load generator panel ----------------------------------------------
  let loadgenPlaceholder = '';
  function onRequestFailed(e) {
    const panel = document.getElementById('loadgen');
    if (panel && panel.contains(e.detail.elt) && panel.querySelector('form')) {
      panel.innerHTML = loadgenPlaceholder; // loadgen went away; the section polls again
    }
  }

  // ---- theme ---------------------------------------------------------------
  const THEMES = ['auto', 'light', 'dark'];
  function storedTheme() {
    try { return localStorage.getItem('theme') || 'auto'; } catch { return 'auto'; }
  }
  function applyTheme(theme) {
    if (theme === 'auto') delete root.dataset.theme; else root.dataset.theme = theme;
    try { localStorage.setItem('theme', theme); } catch { /* private mode */ }
    document.getElementById('theme').textContent = `Theme: ${theme}`;
    readTokens();
    buildAll();
  }

  document.addEventListener('DOMContentLoaded', () => {
    readTokens();
    for (const canvas of document.querySelectorAll('canvas[data-chart]')) {
      lineCharts.push({ canvas, spec: JSON.parse(canvas.dataset.chart), chart: null });
    }
    const panel = document.getElementById('loadgen');
    if (panel) loadgenPlaceholder = panel.innerHTML;

    const themeButton = document.getElementById('theme');
    themeButton.addEventListener('click', () => {
      const current = storedTheme();
      applyTheme(THEMES[(THEMES.indexOf(current) + 1) % THEMES.length]);
    });
    window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => { readTokens(); buildAll(); });
    applyTheme(storedTheme());

    document.body.addEventListener('htmx:sseBeforeMessage', onSSE);
    document.body.addEventListener('htmx:sseOpen', () => setLive('live', 'Live'));
    document.body.addEventListener('htmx:sseError', () => setLive('down', 'Reconnecting'));
    document.body.addEventListener('htmx:sendError', onRequestFailed);
    document.body.addEventListener('htmx:responseError', onRequestFailed);
  });
})();
