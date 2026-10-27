# STM32 core 3.x moved avr/pgmspace.h to cores/arduino/api/deprecated-avr-comp, but PlatformIO's
# build script doesn't put that folder on the include path; GxEPD2 includes <avr/pgmspace.h>.
# Done here instead of a -I build flag because the flag parser eats the backslashes of Windows paths.
from os.path import join

Import("env")

framework_dir = env.PioPlatform().get_package_dir("framework-arduinoststm32")
env.Append(CPPPATH=[join(framework_dir, "cores", "arduino", "api", "deprecated-avr-comp")])
