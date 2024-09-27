window.onload = function () {
  class CircularBuffer {
    constructor(bufferLength) {
      this.buffer = new Array(bufferLength);
      this.head = 0;
      this.size = 0;
      this.bufferLength = bufferLength;
    }

    push(item) {
      if (this.size === this.bufferLength) {
        // Buffer is full, overwrite the oldest item
        this.head = (this.head + 1) % this.bufferLength;
      } else {
        this.size++;
      }

      const insertIndex = (this.head + this.size - 1) % this.bufferLength;
      this.buffer[insertIndex] = item;
    }

    get(index) {
      if (index < 0 || index >= this.size) {
        return null;
      }
      const actualIndex = (this.head + index) % this.bufferLength;
      return this.buffer[actualIndex];
    }
  }

  const donut = document.getElementById("donut");

  function drawFramesToCanvas() {
    let i = 0;
    setInterval(() => {
      donut.innerHTML = frameBuffer.get(i);
      i = Math.floor((i + 1) % frameBuffer.size);
    }, 59);
  }

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

      if (currentFrame.length === 1760) {
        decodedFrames.push(currentFrame);
        currentFrame = "";
      }
    });

    if (currentFrame.length > 0) {
      decodedFrames.push(currentFrame);
    }

    return decodedFrames;
  }

  const ws = new WebSocket("ws://localhost:8080/connect");
  ws.binaryType = "arraybuffer";
  ws.onopen = function () {
    console.log("Connection established");
  };
  const frameBuffer = new CircularBuffer(20 * 60);

  const worker = new Worker("donut-worker.js");
  worker.onmessage = function (e) {
    const encodedData = e.data;
    console.log("Render Task done, sending Render Result to server");
    ws.send(encodedData);
  };

  ws.onmessage = function (e) {
    const data = e.data;

    const dv = new DataView(data);

    const messageType = dv.getUint8(0);

    if (messageType === 0x0) {
      const renderTaskID = dv.getUint16(1);
      const startFrame = dv.getUint32(3);
      const endFrame = dv.getUint32(7);
      console.log(
        "Received Render Task for frames from " + startFrame + " to " + endFrame
      );
      worker.postMessage({ renderTaskID, startFrame, endFrame });
    } else if (messageType === 0x2) {
      // Received frame broadcast message
      console.log("Received a broadcast of frames. Drawing to canvas...");

      const encodedData = new Uint8Array(data, 1); // Skip the message type byte
      const decodedFrames = decodeFrames(encodedData);

      // Add the decoded frames to the circular buffer
      decodedFrames.forEach((frame) => frameBuffer.push(frame));

      // Start drawing the frames to the canvas
      drawFramesToCanvas();
    } else {
      console.log("Received other message");
    }
  };
  ws.onclose = function () {
    console.log("Connection closed");
  };
};
