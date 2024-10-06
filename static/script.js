window.onload = function () {
  const FramesPerBatch = 60;
  const BufferSize = 20 * FramesPerBatch; // 20 seconds worth of frames
  const DonutStringLength = 1760;
  const IntervalBetweenFrames = 1000 / FramesPerBatch;

  const FirstSecondsToBroadcast = 6;
  const SecondsToBroadcast = 4;
  const ClientBufferWindow = FirstSecondsToBroadcast - SecondsToBroadcast;

  class CircularBuffer {
    constructor() {
      this.frames = new Array(BufferSize);
      this.head = 0;
      this.tail = 0;
    }

    push(frames) {
      console.log("Current delta: ", this.getDelta());

      let newHeadPosition = 0;
      for (let i = 0; i < frames.length; i++) {
        newHeadPosition = (this.head + 1) % BufferSize;
        this.frames[newHeadPosition] = frames[i];
        this.head = newHeadPosition;
      }
    }

    get() {
      const frame = this.frames[this.tail];
      this.tail = (this.tail + 1) % BufferSize;
      return frame;
    }

    getDelta() {
      if (this.head >= this.tail) {
        return (this.head - this.tail) / FramesPerBatch;
      } else {
        return (BufferSize - this.tail + this.head) / FramesPerBatch;
      }
    }
  }

  const frameBuffer = new CircularBuffer();
  const donut = document.getElementById("donut");

  function drawFramesToCanvas() {
    let lastFrameTime = 0;

    function animate(currentTime) {
      // Calculate time since last frame
      const deltaTime = currentTime - lastFrameTime;

      // If enough time has passed, draw the next frame
      if (deltaTime >= IntervalBetweenFrames) {
        if (frameBuffer.head !== frameBuffer.tail) {
          const newFrame = frameBuffer.get();
          if (newFrame !== undefined) {
            donut.innerHTML = newFrame;
          }
        }
        // Update last frame time
        lastFrameTime = currentTime;
      }

      // Schedule the next frame
      requestAnimationFrame(animate);
    }

    // Start the animation loop
    requestAnimationFrame(animate);
  }
  drawFramesToCanvas();

  const possibleCharacters = ".,-~:;=!*#$@ \n";

  // Decoding frames
  function decodeFrames(encodedFrames) {
    let currentFrame = "";
    let decodedFrames = [];

    // encodedFrames.slice(3).forEach((byte) => { // Skip the first three bytes, the first for messageType and the other two are for renderTaskID, only do this when decoding the encoded frames on the same machine (i.e. when debugging)
    encodedFrames.forEach((byte) => {
      const highNibble = (byte >> 4) & 0x0f;
      const lowNibble = byte & 0x0f;

      const char1 = possibleCharacters[highNibble];
      const char2 = possibleCharacters[lowNibble];

      if (char1 === undefined || char2 === undefined) {
        throw new Error(`Invalid nibble value: ${highNibble} or ${lowNibble}`);
      }

      currentFrame += char1 + char2;

      if (currentFrame.length === DonutStringLength) {
        decodedFrames.push(currentFrame);
        currentFrame = "";
      }
    });

    if (currentFrame.length > 0) {
      decodedFrames.push(currentFrame);
    }

    return decodedFrames;
  }

  function connectToServer() {
    const host = window.location.host;
    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    return new WebSocket(`${protocol}//${host}/connect`);
  }

  let ws;
  function setupWebSocket() {
    ws = connectToServer();
    ws.binaryType = "arraybuffer";

    ws.onopen = function () {
      console.log("Connection established");
    };

    ws.onmessage = function (e) {
      const data = e.data;
      const dv = new DataView(data);
      const messageType = dv.getUint8(0);

      if (messageType === 0x0) {
        const renderTaskID = dv.getUint32(1);
        const startFrame = dv.getUint32(5);
        const endFrame = dv.getUint32(9);

        console.log(
          "Received Render Task for frames from",
          startFrame,
          "to",
          endFrame
        );
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

    ws.onclose = function () {
      console.log("Connection closed. Attempting to reconnect...");
      setTimeout(setupWebSocket, 3000); // Try to reconnect after 1 second
    };

    ws.onerror = function (error) {
      console.error("WebSocket error:", error);
      ws.close(); // This will trigger onclose, which will attempt to reconnect
    };
  }

  setupWebSocket(); // Initial connection attempt

  const worker = new Worker("donut-worker.js");
  worker.onmessage = function (e) {
    const encodedData = e.data;
    console.log("Render Task done, sending Render Result to server");
    ws.send(encodedData);
  };
};
