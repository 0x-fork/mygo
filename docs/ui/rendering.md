# Rendering

MyGo draws on the GPU with Metal on macOS, with Direct3D 11 on Windows, or
with WARP, Windows' own software renderer, where no GPU driver works, and
with OpenGL on Linux, in the GtkGLArea GTK shows. A shader computes rounded
rectangles, borders, gradients and shadows from the distance to their
edges, so they stay sharp at any size and scale, and text comes from a
glyph atlas that only uploads what changes.

On macOS, frames that change little, such as a clock ticking, typing or the
pointer over a button, are drawn on the CPU, which redraws only what
changed, and go to the screen without waking the GPU: they take less time
than the GPU takes to start, and spare the memory Metal's driver holds for
a couple of seconds after each frame it draws. Scrolling, resizing and
animations of much of the window use the GPU.

Where OpenGL would not run on a GPU, as in virtual machines or in WSL
(where `GALLIUM_DRIVER=d3d12` gives Mesa the GPU), Linux draws the same
pixels on the CPU: a few milliseconds for a whole large window on a
high-density display, and less than a tenth of one for what typically
changes, such as a button under the pointer, since it redraws only that.
Set `MYGO_GPU=0` to use the CPU renderer everywhere, for instance to
compare, and on Linux `MYGO_GPU=1` to draw with OpenGL even where it runs on
the CPU.

MyGo draws a frame only when something changes: input, `Invalidate`,
`After`, or an animation that moves. An idle window draws nothing.
