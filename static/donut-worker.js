// Credit to a1k0n for the origin of the spinning donut: https://www.a1k0n.net/2011/07/20/donut-math.html
const asciiframe = function (frameNumber) {
  const A = 1 + 0.07 * frameNumber;
  const B = 1 + 0.03 * frameNumber;
  const b = [];
  const z = [];
  const cA = Math.cos(A),
    sA = Math.sin(A),
    cB = Math.cos(B),
    sB = Math.sin(B);
  for (let k = 0; k < 1760; k++) {
    b[k] = k % 80 == 79 ? "\n" : " ";
    z[k] = 0;
  }
  for (let j = 0; j < 6.28; j += 0.07) {
    // j <=> theta
    const ct = Math.cos(j),
      st = Math.sin(j);
    for (let i = 0; i < 6.28; i += 0.02) {
      // i <=> phi
      const sp = Math.sin(i),
        cp = Math.cos(i),
        h = ct + 2, // R1 + R2*cos(theta)
        D = 1 / (sp * h * sA + st * cA + 5), // this is 1/z
        t = sp * h * cA - st * sA; // this is a clever factoring of some of the terms in x' and y'

      const x = 0 | (40 + 30 * D * (cp * h * cB - t * sB)),
        y = 0 | (12 + 15 * D * (cp * h * sB + t * cB)),
        o = x + 80 * y,
        N =
          0 |
          (8 *
            ((st * sA - sp * ct * cA) * cB -
              sp * ct * sA -
              st * cA -
              cp * ct * sB));
      if (y < 22 && y >= 0 && x >= 0 && x < 79 && D > z[o]) {
        z[o] = D;
        b[o] = ".,-~:;=!*#$@"[N > 0 ? N : 0];
      }
    }
  }
  return b.join("");
};

function createFrames(startFrame, endFrame) {
  const frames = new Array(endFrame - startFrame + 1);

  for (let i = startFrame; i <= endFrame; i++) {
    frames[i - startFrame] = asciiframe(i);
  }
  return frames;
}

const possibleCharacters = ".,-~:;=!*#$@ \n";

// Encoding frames
function encodeRenderResult(renderTaskID, frames) {
  const encodedFrames = frames.flatMap((frame) => {
    const bytes = [];
    for (let i = 0; i < frame.length; i += 2) {
      const char1 = frame[i];
      const char2 = i + 1 < frame.length ? frame[i + 1] : 12; // possibleCharacters[12]=space ; Used here if there's no second character

      const index1 = possibleCharacters.indexOf(char1);
      const index2 = possibleCharacters.indexOf(char2);

      if (index1 === -1 || index2 === -1) {
        throw new Error(`Invalid character found: ${char1} or ${char2}`);
      }

      const byte = (index1 << 4) | index2;
      bytes.push(byte);
    }
    return bytes;
  });
  const result = new Uint8Array(encodedFrames.length + 5); // 1 for message type, 4 for renderTaskID, and the rest is the encoded frames

  result.set(encodedFrames, 5);
  result[0] = 0x1; // messageType: MessageTypeRenderResult
  // Big endian encoding
  result[1] = (renderTaskID >> 24) & 0xff;
  result[2] = (renderTaskID >> 16) & 0xff;
  result[3] = (renderTaskID >> 8) & 0xff;
  result[4] = renderTaskID & 0xff;
  return result;
}

self.onmessage = function (e) {
  const { renderTaskID, startFrame, endFrame } = e.data;
  const renderResult = createFrames(startFrame, endFrame);
  const encodedData = encodeRenderResult(renderTaskID, renderResult);
  self.postMessage(encodedData, [encodedData.buffer]);
};
