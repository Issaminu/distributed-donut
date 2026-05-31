window.onload = () => {
  const FramesPerBatch = 60;
  const BufferSize = 20 * FramesPerBatch; // 20 seconds worth of frames
  const DonutStringLength = 1760;
  const IntervalBetweenFrames = 1000 / FramesPerBatch;
  // Cap how many frames a single rAF callback may fast-forward, so a long gap
  // (e.g. a backgrounded tab) snaps back to live instead of replaying stale frames.
  const MaxCatchUpFrames = FramesPerBatch;

  class CircularBuffer {
    constructor() {
      this.frames = new Array(BufferSize);
      this.head = 0; // next write slot
      this.tail = 0; // next read slot
      this.count = 0; // buffered frames; the source of truth for full vs. empty
    }

    push(frames) {
      for (let i = 0; i < frames.length; i++) {
        if (this.count === BufferSize) {
          // Full: drop the oldest frame so we stay at the live edge instead of
          // lapping the read pointer and stalling.
          this.tail = (this.tail + 1) % BufferSize;
          this.count--;
        }
        this.frames[this.head] = frames[i];
        this.head = (this.head + 1) % BufferSize;
        this.count++;
      }
    }

    get() {
      if (this.count === 0) return undefined;
      const frame = this.frames[this.tail];
      this.tail = (this.tail + 1) % BufferSize;
      this.count--;
      return frame;
    }

    size() {
      return this.count;
    }

    getDelta() {
      return this.count / FramesPerBatch;
    }
  }

  const frameBuffer = new CircularBuffer();
  const donut = document.getElementById("donut");

  // Live telemetry surfaced in the UI (all read from real client state).
  let framesPlayed = 0;
  let lastTaskID = null;

  function drawFramesToCanvas() {
    let lastFrameTime = performance.now();
    // Fractional count of frames we owe playback, accumulated from wall-clock
    // time so the donut holds a true 60 fps regardless of the display's refresh
    // rate (drawing at most one frame per rAF callback would drift below 60 fps
    // on high-refresh displays, letting the buffer grow without bound).
    let frameDebt = 0;

    function animate(currentTime) {
      const deltaTime = currentTime - lastFrameTime;
      lastFrameTime = currentTime;
      frameDebt += deltaTime / IntervalBetweenFrames;

      let toDraw = Math.floor(frameDebt);
      frameDebt -= toDraw; // carry the sub-frame remainder into the next callback
      if (toDraw > MaxCatchUpFrames) {
        toDraw = MaxCatchUpFrames;
        frameDebt = 0; // drop the backlog after a long gap rather than spiral
      }

      while (toDraw-- > 0 && frameBuffer.size() > 0) {
        const newFrame = frameBuffer.get();
        if (newFrame !== undefined) {
          donut.textContent = newFrame;
          framesPlayed++;
        }
      }

      requestAnimationFrame(animate);
    }

    requestAnimationFrame(animate);
  }
  drawFramesToCanvas();

  const possibleCharacters = ".,-~:;=!*#$@ \n";

  // Decoding frames
  function decodeFrames(encodedFrames) {
    const decodedFrames = [];
    const chars = [];

    for (let i = 0; i < encodedFrames.length; i++) {
      const byte = encodedFrames[i];
      const highNibble = (byte >> 4) & 0x0f;
      const lowNibble = byte & 0x0f;

      const char1 = possibleCharacters[highNibble];
      const char2 = possibleCharacters[lowNibble];

      if (char1 === undefined || char2 === undefined) {
        throw new Error(`Invalid nibble value: ${highNibble} or ${lowNibble}`);
      }

      chars.push(char1, char2);

      if (chars.length === DonutStringLength) {
        decodedFrames.push(chars.join(""));
        chars.length = 0; // reuse the array for the next frame
      }
    }

    if (chars.length > 0) {
      decodedFrames.push(chars.join(""));
    }

    return decodedFrames;
  }

  function connectToServer() {
    const host = window.location.host;
    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    return new WebSocket(`${protocol}//${host}/ws`);
  }

  let ws;
  function setupWebSocket() {
    ws = connectToServer();
    ws.binaryType = "arraybuffer";

    ws.onopen = () => {
      console.log("Connection established");
      setStatus("online", "online · rendering");
    };

    ws.onmessage = (e) => {
      const data = e.data;
      const dv = new DataView(data);
      const messageType = dv.getUint8(0);

      if (messageType === 0x0) {
        const renderTaskID = dv.getUint32(1);
        const startFrame = dv.getUint32(5);
        const endFrame = dv.getUint32(9);

        console.log(
          "Received Render Task",
          renderTaskID,
          "for frames from",
          startFrame,
          "to",
          endFrame
        );
        lastTaskID = renderTaskID;
        worker.postMessage({ renderTaskID, startFrame, endFrame });
      } else if (messageType === 0x2) {
        // Received frame broadcast message
        console.log("Received a broadcast of frames. Drawing to canvas...");

        const encodedData = new Uint8Array(data, 1); // Skip the message type byte
        const decodedFrames = decodeFrames(encodedData);

        // Add the decoded frames to the circular buffer
        frameBuffer.push(decodedFrames);
      } else {
        console.log("Received other message");
      }
    };

    ws.onclose = () => {
      console.log("Connection closed. Attempting to reconnect...");
      setStatus("offline", "reconnecting");
      setTimeout(setupWebSocket, 3000); // Try to reconnect after 1 second
    };

    ws.onerror = (error) => {
      console.error("WebSocket error:", error);
      ws.close(); // This will trigger onclose, which will attempt to reconnect
    };
  }

  // --- UI telemetry -------------------------------------------------------
  const statusEl = document.getElementById("status");
  const statusText = document.getElementById("status-text");
  const tBuffer = document.getElementById("t-buffer");
  const tFps = document.getElementById("t-fps");
  const tFrames = document.getElementById("t-frames");
  const tTask = document.getElementById("t-task");
  const sparkLine = document.getElementById("spark-line");
  const sparkArea = document.getElementById("spark-area");

  function setStatus(state, text) {
    if (!statusEl) return;
    statusEl.classList.remove("online", "offline");
    statusEl.classList.add(state);
    statusText.textContent = text;
  }

  // Rolling history of the buffer depth, drawn as a sparkline (viewBox 100x40).
  const SPARK_SAMPLES = 40;
  const bufferHistory = [];

  function drawSpark() {
    if (!sparkLine) return;
    const n = bufferHistory.length;
    if (n < 2) {
      sparkLine.setAttribute("points", "");
      if (sparkArea) sparkArea.setAttribute("points", "");
      return;
    }
    const max = Math.max(4, ...bufferHistory) * 1.1;
    let pts = "";
    for (let i = 0; i < n; i++) {
      const x = (i / (n - 1)) * 100;
      const y = 39 - (bufferHistory[i] / max) * 37;
      pts += (i ? " " : "") + x.toFixed(1) + "," + y.toFixed(1);
    }
    sparkLine.setAttribute("points", pts);
    if (sparkArea) sparkArea.setAttribute("points", "0,40 " + pts + " 100,40");
  }

  // Sample the real client state once a second, off the render hot path.
  let prevFrames = 0;
  let prevTime = performance.now();
  setInterval(() => {
    const now = performance.now();
    const fps = Math.round(((framesPlayed - prevFrames) * 1000) / (now - prevTime));
    prevFrames = framesPlayed;
    prevTime = now;

    const buffered = frameBuffer.getDelta();
    if (tFps) tFps.textContent = fps;
    if (tBuffer) tBuffer.textContent = buffered.toFixed(1);
    if (tFrames) tFrames.textContent = framesPlayed.toLocaleString();
    if (tTask) tTask.textContent = lastTaskID === null ? "—" : lastTaskID;

    bufferHistory.push(buffered);
    if (bufferHistory.length > SPARK_SAMPLES) bufferHistory.shift();
    drawSpark();
  }, 1000);

  setupWebSocket(); // Initial connection attempt

  const worker = new Worker("donut-worker.js");
  worker.onmessage = (e) => {
    const encodedData = e.data;
    console.log("Render Task done, sending Render Result to server");
    ws.send(encodedData);
  };
};
