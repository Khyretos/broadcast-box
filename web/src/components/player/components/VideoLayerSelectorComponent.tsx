import { ChangeEvent, useState } from "react";
import { ChartBarIcon } from "@heroicons/react/16/solid";
import { VideoLayerInfo } from "../functions/peerconnection";

interface QualityComponentProps {
	layers: VideoLayerInfo[];
	layerEndpoint: string;
	hasPacketLoss: boolean;
	currentLayer: string;
}

// "0 - 1080p @ 120fps, 12 Mb/s", or just the layer id until the quality is known
export const formatLayerLabel = (layer: VideoLayerInfo) => {
	const details: string[] = [];
	if (layer.height) {
		details.push(`${layer.height}p` + (layer.framesPerSecond ? ` @ ${Math.round(layer.framesPerSecond)}fps` : ""));
	} else if (layer.framesPerSecond) {
		details.push(`${Math.round(layer.framesPerSecond)}fps`);
	}
	if (layer.bitrate) {
		const megabits = layer.bitrate / 1_000_000;
		details.push(`${megabits >= 10 ? Math.round(megabits) : megabits.toFixed(1)} Mb/s`);
	}

	const name = layer.encodingId || "Video";
	return details.length > 0 ? `${name} - ${details.join(", ")}` : name;
};

const VideoLayerSelectorComponent = (props: QualityComponentProps) => {
	const videoMediaId = "1"
	const [isOpen, setIsOpen] = useState<boolean>(false);

	const onLayerChange = (event: ChangeEvent<HTMLSelectElement>) => {
		fetch(props.layerEndpoint, {
			method: 'POST',
			body: JSON.stringify({ mediaId: videoMediaId, encodingId: event.target.value }),
			headers: {
				'Content-Type': 'application/json'
			}
		}).catch((err) => console.error("VideoLayerSelectorComponent.onLayerChange", err))
		setIsOpen(false)
	}

	const currentLayer = props.currentLayer
	const current = props.layers.find((layer) => layer.encodingId === currentLayer)
	const layerList = [
		...(current ? [current] : []),
		...props.layers.filter(layer => layer.encodingId !== currentLayer)
	].map(layer => <option key={`layerEncodingId_${layer.encodingId}`} value={layer.encodingId}>{formatLayerLabel(layer)}</option>)

	// With a single layer that one is playing, even on "Auto"
	const labelled = current ?? (props.layers.length === 1 ? props.layers[0] : undefined)

	if (currentLayer === '' || !current) {
		layerList.unshift(<option key="disabled">Auto</option>)
	}

	return (
		<div className="h-full flex" title={labelled ? formatLayerLabel(labelled) : undefined}>
			<ChartBarIcon
				className={props.hasPacketLoss ? "text-orange-600" : "" + (props.layers.length === 0 ? "opacity-25" : "")}
				onClick={() => setIsOpen((prev) => props.layers.length <= 1 ? false : !prev)} />

			{isOpen && (
				<select
					onChange={onLayerChange}
					value={currentLayer}
					className="
						absolute 
						right-0
						bottom-8
						min-w-50
						w-auto
						appearance-none
						border
						py-2
						px-3
						leading-tight
						focus:outline-hidden
						focus:shadow-outline
						bg-gray-700
						border-gray-700
						text-white
						rounded-sm
						shadow-md
						placeholder-gray-200">
					{
						layerList
					}
				</select>
			)}
		</div>
	)
}

export default VideoLayerSelectorComponent
